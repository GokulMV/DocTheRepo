package rag_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/core/sift"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
)

// siftEnv routes judgments to a cheaper model; judge says yes to items mentioning want.
func siftEnv(t *testing.T, want string, answer func(prompt string) string) (*fakegw.Env, *[]string) {
	env := fakegw.New(fakegw.Options{})
	env.Routes[llmgateway.FeatureSift] = llmgateway.Route{ProviderID: "llm", ProviderKind: "anthropic", Model: "claude-haiku-4-5"}
	var prompts []string
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		last := r.Messages[len(r.Messages)-1].Content
		if r.System == llmgateway.JudgeSystem {
			state, _, _ := strings.Cut(strings.TrimPrefix(last, "## State\n"), "\n\n## Questions")
			var st struct {
				Items []struct {
					Kind, Path, Text string
					Entries          []string
				} `json:"items"`
			}
			require.NoError(t, json.Unmarshal([]byte(state), &st))
			p := map[string]float64{}
			for _, line := range strings.Split(last, "\n") {
				id, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), ":")
				if !ok || !strings.HasPrefix(line, "- ") {
					continue
				}
				var n int
				_, _ = fmt.Sscanf(strings.TrimLeft(id, "relscopedirfile"), "%d", &n)
				it := st.Items[n]
				hay := it.Text
				if it.Kind != "code" {
					hay += " " + it.Path + " " + strings.Join(it.Entries, " ")
				}
				p[id] = 0.03
				if strings.Contains(hay, want) {
					p[id] = 0.95
				}
			}
			b, _ := json.Marshal(map[string]any{"p": p})
			return ports.ChatResponse{Text: string(b), Usage: ports.TokenUsage{InputTokens: 400, Reported: true}}, nil
		}
		if r.JSONSchema != nil {
			t.Fatalf("the agent should not be needed: %.300s", last)
		}
		prompts = append(prompts, last)
		text := answer(last)
		if r.OnDelta != nil {
			r.OnDelta(text)
		}
		return ports.ChatResponse{Text: text, Usage: ports.TokenUsage{InputTokens: 1000, OutputTokens: 20}}, nil
	}
	return env, &prompts
}

func TestAskReadsOnlySiftedSources(t *testing.T) {
	st := &memStore{}
	for i := 0; i < 20; i++ {
		body := fmt.Sprintf("func retry%d() // retry helper used by many callers", i)
		if i == 7 {
			body = "func Backoff() // payment retry uses exponential backoff, capped at 30 seconds"
		}
		st.chunks = append(st.chunks, chunk(fmt.Sprintf("c%d", i), fmt.Sprintf("pkg/r%d.go", i), body+strings.Repeat("\n// filler", 50)))
	}
	env, prompts := siftEnv(t, "exponential backoff", func(p string) string {
		if strings.Contains(p, "exponential backoff") {
			return "Payment retries back off exponentially, capped at 30 seconds [1]."
		}
		return rag.NotFoundAnswer
	})
	e := &rag.Engine{Store: st, GW: env.GW, AgentSteps: 4, Sift: &sift.Sifter{GW: env.GW, MinTokens: 1}}
	a, err := e.Ask(context.Background(), rag.Query{Question: "how does payment retry work", Scope: rag.Scope{All: true}})
	require.NoError(t, err)
	require.Len(t, a.Citations, 1)
	assert.Equal(t, "pkg/r7.go", a.Citations[0].Path)
	require.Len(t, *prompts, 1)
	assert.Equal(t, 1, strings.Count((*prompts)[0], "<source "), "the answering model read one source, not twenty")
	require.NotNil(t, a.Sift)
	assert.Equal(t, 20, a.Sift.Candidates)
	assert.Equal(t, 1, a.Sift.Kept)
	assert.Positive(t, a.Sift.TokensSaved)
	assert.False(t, a.Investigated)
}

func TestAskExploresBeforeTheAgent(t *testing.T) {
	st := &memStore{}
	for i := 0; i < 45; i++ {
		st.chunks = append(st.chunks, chunk(fmt.Sprintf("w%d", i), fmt.Sprintf("web/ui/w%d.tsx", i), "export const Widget = () => null"))
	}
	st.chunks = append(st.chunks,
		chunk("rot", "internal/keys/rotation.go", "func Rotate() // signing keys rotate every 24 hours from a cron job"),
		chunk("rot2", "internal/keys/schedule.go", "func schedule() // registers the nightly signing keys job"))
	env, prompts := siftEnv(t, "keys", func(p string) string {
		if strings.Contains(p, "24 hours") {
			return "Signing keys rotate every 24 hours [1][2]."
		}
		return rag.NotFoundAnswer
	})
	e := &rag.Engine{Store: st, GW: env.GW, AgentSteps: 4, Sift: &sift.Sifter{GW: env.GW, MinTokens: 1}}
	// "credential renewal" shares no words with the code, so search finds nothing.
	a, err := e.Ask(context.Background(), rag.Query{Question: "when does credential renewal happen", Scope: rag.Scope{All: true}})
	require.NoError(t, err)
	assert.True(t, a.Investigated)
	require.NotEmpty(t, a.Citations, a.Text)
	assert.Contains(t, a.Citations[0].Path, "internal/keys/")
	assert.Len(t, *prompts, 1, "one answering call; no agent steps")
	require.NotNil(t, a.Sift)
	assert.Equal(t, 2, a.Sift.Explored)
	for _, r := range st.reads {
		assert.NotContains(t, r, "web/", "the web directory was never opened")
	}
}

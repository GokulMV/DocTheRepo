package sift_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/sift"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
)

var stateRE = regexp.MustCompile(`(?s)## State\n(.*)\n\n## Questions`)

// judgeOn answers judgments like a sensible small model: an item is relevant and in scope when its text
// or path mentions any of the words; everything else is no. Chat requests without a schema fail the test.
func judgeOn(t *testing.T, env *fakegw.Env, words ...string) *int {
	calls := 0
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		require.NotNil(t, r.JSONSchema, "only judgments are expected")
		calls++
		m := stateRE.FindStringSubmatch(r.Messages[0].Content)
		require.Len(t, m, 2)
		var st struct {
			Items []struct{ Kind, Path, Text string } `json:"items"`
		}
		require.NoError(t, json.Unmarshal([]byte(m[1]), &st))
		p := map[string]float64{}
		for i, it := range st.Items {
			v := 0.05
			for _, w := range words {
				hay := it.Text + " " + it.Path
				if it.Kind == "code" { // a piece is judged on its text; directories and files also by path
					hay = it.Text
				}
				if strings.Contains(strings.ToLower(hay), w) {
					v = 0.92
				}
			}
			for _, prefix := range []string{"rel", "scope", "dir", "file"} {
				p[fmt.Sprintf("%s%d", prefix, i)] = v
			}
		}
		// only answer what was asked (the schema forbids extra keys)
		asked := map[string]float64{}
		for _, line := range strings.Split(r.Messages[0].Content, "\n") {
			if id, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), ":"); ok && strings.HasPrefix(line, "- ") {
				asked[id] = p[id]
			}
		}
		b, _ := json.Marshal(map[string]any{"p": asked})
		return ports.ChatResponse{Text: string(b), Usage: ports.TokenUsage{InputTokens: 500, OutputTokens: 20, Reported: true}}, nil
	}
	return &calls
}

func newEnv() *fakegw.Env {
	env := fakegw.New(fakegw.Options{})
	env.Routes[llmgateway.FeatureDecide] = llmgateway.Route{Feature: "decide", ProviderID: "llm", ProviderKind: "anthropic", Model: "claude-haiku-4-5"}
	return env
}

func pieces(n int, relevant map[int]bool) []ports.Chunk {
	var out []ports.Chunk
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("func helper%d() { /* unrelated formatting code */ }", i)
		if relevant[i] {
			body = fmt.Sprintf("func rotate%d() { // key rotation runs nightly from the scheduler", i)
		}
		out = append(out, ports.Chunk{ID: fmt.Sprintf("c%d", i), Scope: "acme/api", Path: fmt.Sprintf("pkg/f%d.go", i),
			Symbol: fmt.Sprintf("s%d", i), Source: ports.SourceCode, Content: body + strings.Repeat("\n// padding line", 60)})
	}
	return out
}

func TestSelectKeepsOnlyWhatTheJudgePassesAndCaches(t *testing.T) {
	env := newEnv()
	calls := judgeOn(t, env, "rotation")
	s := &sift.Sifter{GW: env.GW, Cache: &sift.Memory{}}
	qa, _ := env.GW.Route(context.Background(), llmgateway.FeatureQA)
	in := pieces(24, map[int]bool{3: true, 17: true})

	kept, rep := s.Select(context.Background(), llmgateway.CallMeta{}, "How does key rotation work?", in, qa)
	require.Empty(t, rep.Skipped)
	assert.Equal(t, []string{"c3", "c17"}, ids(kept), "relevant pieces only, in retrieval order on ties")
	assert.Equal(t, 24, rep.Candidates)
	assert.Equal(t, 3, rep.Calls, "24 pieces in batches of 10")
	assert.Equal(t, 3, *calls)
	assert.Less(t, rep.TokensOut, rep.TokensIn/5)
	assert.Equal(t, 3, env.Ledger.Calls(llmgateway.FeatureSift), "recorded as sift, not decide")
	assert.False(t, rep.Calibrated, "a chat judge is self-reported")

	_, rep = s.Select(context.Background(), llmgateway.CallMeta{}, "how does KEY rotation  work?", in, qa)
	assert.Equal(t, 24, rep.CacheHits, "the same question in other case and spacing re-judges nothing")
	assert.Equal(t, 3, *calls)
}

func TestSelectNeverLosesSources(t *testing.T) {
	ctx := context.Background()
	in := pieces(12, map[int]bool{1: true})

	env := newEnv()
	env.Model.Handler = func(ports.ChatRequest) (ports.ChatResponse, error) { return ports.ChatResponse{}, errors.New("boom") }
	qa, _ := env.GW.Route(ctx, llmgateway.FeatureQA)
	kept, rep := (&sift.Sifter{GW: env.GW}).Select(ctx, llmgateway.CallMeta{}, "rotation?", in, qa)
	assert.Len(t, kept, 12, "a failed judge keeps every source")
	assert.Contains(t, rep.Skipped, "judge failed")

	env = newEnv()
	judgeOn(t, env, "nothing-matches")
	kept, rep = (&sift.Sifter{GW: env.GW}).Select(ctx, llmgateway.CallMeta{}, "rotation?", in, qa)
	assert.True(t, rep.Weak)
	assert.Len(t, kept, 3, "nothing passed: the best few remain so the answer can say so")

	kept, rep = (&sift.Sifter{GW: env.GW}).Select(ctx, llmgateway.CallMeta{}, "rotation?", in[:1], qa)
	assert.Len(t, kept, 1)
	assert.Contains(t, rep.Skipped, "too little")
}

func TestSkipsWhenJudgingWouldNotSave(t *testing.T) {
	ctx := context.Background()
	in := pieces(12, nil)
	env := fakegw.New(fakegw.Options{})
	qa, _ := env.GW.Route(ctx, llmgateway.FeatureQA)
	_, rep := (&sift.Sifter{GW: env.GW}).Select(ctx, llmgateway.CallMeta{}, "q", in, qa)
	assert.Contains(t, rep.Skipped, "no judge route")

	env.Routes[llmgateway.FeatureSift] = qa
	_, rep = (&sift.Sifter{GW: env.GW}).Select(ctx, llmgateway.CallMeta{}, "q", in, qa)
	assert.Contains(t, rep.Skipped, "is the answering model")

	env = newEnv()
	price := map[string]float64{"claude-opus-5-5": 5, "claude-haiku-4-5": 4}
	cost := func(_, model, _ string, in, _ int64) (float64, bool) { return price[model] * float64(in) / 1e6, true }
	_, rep = (&sift.Sifter{GW: env.GW, Cost: cost}).Select(ctx, llmgateway.CallMeta{}, "q", in, qa)
	assert.Contains(t, rep.Skipped, "costs more than 50%")
}

type tree struct{ files map[sift.File][]ports.Chunk }

func (t tree) Paths(context.Context, int) ([]sift.File, error) {
	var out []sift.File
	for f := range t.files {
		out = append(out, f)
	}
	return out, nil
}
func (t tree) Read(_ context.Context, f sift.File, limit int) ([]ports.Chunk, error) {
	cs := t.files[f]
	return cs[:min(limit, len(cs))], nil
}

func TestNavigateDescendsOnlyIntoPromisingDirectories(t *testing.T) {
	tr := tree{files: map[sift.File][]ports.Chunk{}}
	add := func(p, body string) {
		f := sift.File{Repo: "acme/api", Path: p}
		tr.files[f] = append(tr.files[f], ports.Chunk{ID: p, Scope: f.Repo, Path: p, Source: ports.SourceCode, Content: body})
	}
	for i := 0; i < 30; i++ {
		add(fmt.Sprintf("web/components/c%d.tsx", i), "export const Button = () => null")
		add(fmt.Sprintf("billing/invoices/i%d.go", i), "func Total() int { return 0 }")
	}
	add("security/keys/rotation.go", "func Rotate() { // key rotation, nightly }")
	add("security/keys/store.go", "func Load() {}")

	env := newEnv()
	judgeOn(t, env, "security", "keys", "rotation")
	qa, _ := env.GW.Route(context.Background(), llmgateway.FeatureQA)
	found, rep := (&sift.Sifter{GW: env.GW}).Navigate(context.Background(), llmgateway.CallMeta{}, "How does key rotation work?", tr, qa)
	require.Empty(t, rep.Skipped)
	assert.Equal(t, []string{"security/keys/rotation.go"}, ids(found))
	assert.LessOrEqual(t, rep.Directories, 6, "web/ and billing/ are never opened")
	assert.Equal(t, 2, rep.Navigated, "only files under security/keys are read")

	env = newEnv()
	judgeOn(t, env, "no-such-thing")
	found, _ = (&sift.Sifter{GW: env.GW}).Navigate(context.Background(), llmgateway.CallMeta{}, "Where is the GPU scheduler?", tr, qa)
	assert.Empty(t, found, "nothing relevant: explore returns nothing rather than padding")
}

func ids(cs []ports.Chunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

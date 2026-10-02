package rag_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
)

// memStore is an in-memory rag.Store and rag.Explorer: full text matches any query word in a chunk.
type memStore struct {
	mu     sync.Mutex
	chunks []ports.Chunk
	puts   int
	reads  []string
}

func (m *memStore) FullText(_ context.Context, q string, _ rag.Scope, k int) ([]ports.VectorHit, error) {
	var out []ports.VectorHit
	for _, c := range m.chunks {
		for _, w := range strings.Fields(strings.ToLower(q)) {
			if len(w) > 3 && strings.Contains(strings.ToLower(c.Content+" "+c.Path), w) {
				out = append(out, ports.VectorHit{ChunkID: c.ID})
				break
			}
		}
	}
	return out, nil
}

func (m *memStore) Chunks(_ context.Context, ids []string, _ rag.Scope) ([]ports.Chunk, error) {
	var out []ports.Chunk
	for _, id := range ids {
		for _, c := range m.chunks {
			if c.ID == id {
				out = append(out, c)
			}
		}
	}
	return out, nil
}
func (m *memStore) SymbolNeighbors(context.Context, []string, int) ([]string, error) { return nil, nil }
func (m *memStore) CachedAnswer(context.Context, string) (rag.Answer, bool, error) {
	return rag.Answer{}, false, nil
}
func (m *memStore) PutAnswer(context.Context, string, rag.Answer) error {
	m.mu.Lock()
	m.puts++
	m.mu.Unlock()
	return nil
}
func (m *memStore) ChunksForPath(_ context.Context, p string, _ rag.Scope, _ int) ([]ports.Chunk, error) {
	m.reads = append(m.reads, p)
	var out []ports.Chunk
	for _, c := range m.chunks {
		if c.Path == p || strings.HasSuffix(c.Path, "/"+p) {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *memStore) ListPaths(_ context.Context, s string, _ rag.Scope, _ int) ([]rag.PathEntry, error) {
	var out []rag.PathEntry
	for _, c := range m.chunks {
		if strings.Contains(c.Path, s) {
			out = append(out, rag.PathEntry{Repo: c.Scope, Path: c.Path, Chunks: 1})
		}
	}
	return out, nil
}

func chunk(id, path, content string) ports.Chunk {
	return ports.Chunk{ID: id, Scope: "acme/tokensmith", Path: path, Symbol: id, Source: ports.SourceCode, Content: content}
}

// When the first search finds too little, the model searches with other words, reads the file it
// found, and answers from it, citing it; the person sees each step.
func TestAgentLooksFurtherWhenSearchFindsTooLittle(t *testing.T) {
	st := &memStore{chunks: []ports.Chunk{
		chunk("readme", "README.md", "Tokensmith issues API tokens."),
		chunk("rotate", "internal/keys/rotation.go", "func Rotate() — rotation of signing keys runs every 24 hours from a cron job."),
		chunk("rotate2", "internal/keys/rotation.go", "func schedule() — registers the nightly key rotation job."),
	}}
	env := fakegw.New(fakegw.Options{})
	var plans []string
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		last := r.Messages[len(r.Messages)-1].Content
		if r.JSONSchema != nil { // the agent's planning step
			plans = append(plans, last)
			step := map[string]string{"action": "answer", "input": "", "reason": "enough"}
			switch {
			case !strings.Contains(last, "Actions so far"):
				step = map[string]string{"action": "search", "input": "rotation signing keys", "reason": "looking for key rotation code"}
			case !strings.Contains(last, "read_file"):
				step = map[string]string{"action": "read_file", "input": "keys/rotation.go", "reason": "reading the rotation file"}
			}
			b, _ := json.Marshal(step)
			return ports.ChatResponse{Text: string(b)}, nil
		}
		text := rag.NotFoundAnswer
		if strings.Contains(last, "every 24 hours") {
			text = "Keys rotate every 24 hours, from a cron job [2]."
		}
		if r.OnDelta != nil {
			r.OnDelta(text)
		}
		return ports.ChatResponse{Text: text, Usage: ports.TokenUsage{InputTokens: 100, OutputTokens: 10}}, nil
	}
	e := &rag.Engine{Store: st, GW: env.GW, AgentSteps: 4}
	var deltas strings.Builder
	var statuses []rag.Status
	a, err := e.Ask(context.Background(), rag.Query{Question: "how often do keys get changed?", Scope: rag.Scope{All: true},
		OnDelta: func(s string) { deltas.WriteString(s) }, OnStatus: func(s rag.Status) { statuses = append(statuses, s) }})
	require.NoError(t, err)
	assert.True(t, a.Investigated)
	assert.Contains(t, a.Text, "every 24 hours")
	require.NotEmpty(t, a.Citations)
	assert.Equal(t, "internal/keys/rotation.go", a.Citations[0].Path)
	assert.NotContains(t, deltas.String(), rag.NotFoundAnswer, "the first round's 'not found' is never shown")
	assert.Equal(t, []string{"search", "read_file"}, []string{statuses[0].Action, statuses[1].Action})
	assert.Equal(t, []string{"keys/rotation.go"}, st.reads)
	assert.Len(t, plans, 3, "two steps, then the decision to answer")
	assert.Contains(t, plans[2], "rotation.go", "each step sees what the earlier ones found")
	assert.Equal(t, 1, st.puts, "the investigated answer is cached, so asking again costs nothing")
}

// With enough material and a cited answer, the agent never runs: one model call.
func TestAgentStaysOutWhenSearchIsEnough(t *testing.T) {
	st := &memStore{chunks: []ports.Chunk{
		chunk("a", "a.go", "token issuing works like this"), chunk("b", "b.go", "token expiry rules"), chunk("c", "c.go", "token storage layer"),
	}}
	env := fakegw.New(fakegw.Options{})
	calls := 0
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		calls++
		return ports.ChatResponse{Text: "Tokens are issued by the issuer [1]."}, nil
	}
	a, err := (&rag.Engine{Store: st, GW: env.GW, AgentSteps: 4}).Ask(context.Background(), rag.Query{Question: "how do token work", Scope: rag.Scope{All: true}})
	require.NoError(t, err)
	assert.False(t, a.Investigated)
	assert.Equal(t, 1, calls)
}

// An uncited first answer that was already streamed is withdrawn (reset) before the agent answers; with
// the agent off, "not found" is final.
func TestAgentResetsAStreamedUncitedAnswer(t *testing.T) {
	st := &memStore{chunks: []ports.Chunk{chunk("a", "a.go", "token one"), chunk("b", "b.go", "token two"), chunk("c", "c.go", "token three")}}
	env := fakegw.New(fakegw.Options{})
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		if r.JSONSchema != nil {
			return ports.ChatResponse{Text: `{"action":"answer","input":"","reason":"nothing more"}`}, nil
		}
		text := "I think tokens are fine."
		r.OnDelta(text)
		return ports.ChatResponse{Text: text}, nil
	}
	resets := 0
	var deltas strings.Builder
	a, err := (&rag.Engine{Store: st, GW: env.GW, AgentSteps: 2}).Ask(context.Background(), rag.Query{Question: "token stuff", Scope: rag.Scope{All: true},
		OnDelta: func(s string) { deltas.WriteString(s) }, OnReset: func() { resets++; deltas.Reset() }})
	require.NoError(t, err)
	assert.Equal(t, 1, resets)
	assert.Equal(t, rag.NotFoundAnswer, a.Text)
	assert.True(t, a.Investigated)

	a, err = (&rag.Engine{Store: st, GW: env.GW}).Ask(context.Background(), rag.Query{Question: "token stuff", Scope: rag.Scope{All: true}, OnDelta: func(string) {}})
	require.NoError(t, err)
	assert.False(t, a.Investigated, "agent off")
}

package decode

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// --- fakes ---

type ledger struct{ recs []ports.UsageRecord }

func (l *ledger) Spent(context.Context, ports.SpendFilter) (ports.SpendTotals, error) {
	return ports.SpendTotals{}, nil
}
func (l *ledger) Record(_ context.Context, r ports.UsageRecord) error {
	l.recs = append(l.recs, r)
	return nil
}

type llm struct {
	replies []string
	prompts []string
}

func (f *llm) Kind() string { return "anthropic" }
func (f *llm) Chat(_ context.Context, r ports.ChatRequest) (ports.ChatResponse, error) {
	f.prompts = append(f.prompts, r.Messages[len(r.Messages)-1].Content)
	if len(f.replies) == 0 {
		return ports.ChatResponse{}, errors.New("no scripted reply")
	}
	t := f.replies[0]
	f.replies = f.replies[1:]
	return ports.ChatResponse{Text: t, Model: "m1", Usage: ports.TokenUsage{InputTokens: 900, OutputTokens: 100, Reported: true}}, nil
}
func (f *llm) Ping(context.Context, string) error { return nil }

type emb struct{}

func (emb) Kind() string  { return "fake" }
func (emb) MaxBatch() int { return 8 }
func (emb) Embed(_ context.Context, _ string, texts []string) (ports.EmbedResponse, error) {
	r := ports.EmbedResponse{Dimensions: 2, Usage: ports.TokenUsage{InputTokens: 1, Reported: true}}
	for range texts {
		r.Vectors = append(r.Vectors, []float32{1, 0})
	}
	return r, nil
}

type providers struct{ l *llm }

func (p providers) Route(_ context.Context, f string) (llmgateway.Route, error) {
	return llmgateway.Route{ProviderID: "p", ProviderKind: "anthropic", Model: "m1", MaxOutputTokens: 1000}, nil
}
func (p providers) LLM(context.Context, string) (ports.LLM, error)           { return p.l, nil }
func (p providers) Embedder(context.Context, string) (ports.Embedder, error) { return emb{}, nil }
func (p providers) DocGenerator(context.Context, string) (ports.DocGenerator, error) {
	return nil, errors.New("unused")
}

type index struct {
	hits map[ports.ChunkSource][]ports.VectorHit
}

func (x *index) Kind() string                                           { return "fake" }
func (x *index) EnsureIndex(context.Context, ports.EmbeddingSpec) error { return nil }
func (x *index) State(context.Context) (ports.IndexState, error)        { return ports.IndexState{}, nil }
func (x *index) Upsert(context.Context, int, []ports.ChunkVector) error { return nil }
func (x *index) Delete(context.Context, []string) error                 { return nil }
func (x *index) Rekey(context.Context, map[string]string) error         { return nil }
func (x *index) Search(_ context.Context, _ []float32, k int, f ports.VectorFilter) ([]ports.VectorHit, error) {
	return x.hits[f.Sources[0]], nil
}
func (x *index) BeginReindex(context.Context, ports.EmbeddingSpec) (int, error) { return 0, nil }
func (x *index) SwapReindex(context.Context, int) error                         { return nil }
func (x *index) AbortReindex(context.Context, int) error                        { return nil }

type mem struct {
	issue    Issue
	samples  []ports.SignalEvent
	chunks   map[string]ports.Chunk
	frames   []ports.StackFrame
	stored   *Stored
	saved    []Record
	put      []ports.Chunk
	owners   map[string]string
	docPaths []string
}

func (m *mem) Issue(context.Context, string) (Issue, error) { return m.issue, nil }
func (m *mem) Samples(context.Context, string, int) ([]ports.SignalEvent, error) {
	return m.samples, nil
}
func (m *mem) ServiceRepos(context.Context, string) ([]string, error) { return []string{"r1"}, nil }
func (m *mem) FrameChunks(_ context.Context, _ []string, fs []ports.StackFrame) ([]ports.Chunk, error) {
	m.frames = fs
	return []ports.Chunk{m.chunks["c-frame"]}, nil
}
func (m *mem) Docs(_ context.Context, _ string, paths []string) ([]ports.Chunk, error) {
	m.docPaths = paths
	return []ports.Chunk{m.chunks["d1"]}, nil
}
func (m *mem) Chunks(_ context.Context, ids []string) ([]ports.Chunk, error) {
	var out []ports.Chunk
	for _, id := range ids {
		if c, ok := m.chunks[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *mem) LatestDecode(context.Context, string) (Stored, bool, error) {
	if m.stored == nil {
		return Stored{}, false, nil
	}
	return *m.stored, true, nil
}
func (m *mem) SaveDecode(_ context.Context, r Record) (string, error) {
	m.saved = append(m.saved, r)
	return "dec-1", nil
}
func (m *mem) PutChunk(_ context.Context, c ports.Chunk) error { m.put = append(m.put, c); return nil }
func (m *mem) IssueForChunks(context.Context, []string) (map[string]string, error) {
	return m.owners, nil
}

type savings struct{ kinds []string }

func (s *savings) Record(_ context.Context, kind string, _ int64, _ float64, _ string) error {
	s.kinds = append(s.kinds, kind)
	return nil
}

func setup(t *testing.T, replies ...string) (*Decoder, *mem, *llm, *savings) {
	t.Helper()
	l := &llm{replies: replies}
	g, err := spendguard.New(nil, nil, true)
	require.NoError(t, err)
	gw := llmgateway.New(spendguard.NewEnforcer(g, &ledger{}, nil), providers{l}, providers{l}, true)
	m := &mem{
		issue: Issue{ID: "iss-1", Kind: "error", Title: "ZeroDivisionError: division by zero", Service: "billing", Status: "new", Occurrences: 42, Sources: []string{"sentry"}},
		samples: []ports.SignalEvent{{Title: "ZeroDivisionError: division by zero", ExceptionType: "ZeroDivisionError", Severity: ports.SeverityError,
			Message: "division by zero", OccurredAt: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
			Stack: []ports.StackFrame{{Module: "billing.invoice", Function: "render", File: "billing/invoice.py", Line: 88, InApp: true}},
			Attrs: map[string]string{"order": "[REDACTED]"}}},
		chunks: map[string]ports.Chunk{
			"c-frame": {ID: "c-frame", RepoID: "r1", Path: "billing/invoice.py", Symbol: "render", Content: "def render(lines):\n    return sum(lines) / len(lines)", ContentHash: "h1"},
			"c-vec":   {ID: "c-vec", RepoID: "r1", Path: "billing/lines.py", Symbol: "load", Content: "def load(): ...", ContentHash: "h2"},
			"d1":      {ID: "d1", RepoID: "r1", Path: "docs/generated/billing/invoice.py.md", Symbol: "render", Source: ports.SourceGeneratedDoc, Content: "Renders an invoice."},
			"sim":     {ID: "sim", Source: ports.SourceIssueDecode, Content: "Issue: ZeroDivisionError in totals\nSummary: empty invoices divide by zero"},
		},
		owners: map[string]string{"sim": "iss-0"},
	}
	x := &index{hits: map[ports.ChunkSource][]ports.VectorHit{
		ports.SourceCode:        {{ChunkID: "c-frame", Score: 0.9}, {ChunkID: "c-vec", Score: 0.8}},
		ports.SourceIssueDecode: {{ChunkID: DecodeChunkID("iss-1"), Score: 0.99}, {ChunkID: "sim", Score: 0.9}, {ChunkID: "weak", Score: 0.5}},
	}}
	sv := &savings{}
	d := &Decoder{Store: m, GW: gw, Index: x, Savings: sv, Now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) },
		Commits: func(_ context.Context, repo, path string, since time.Time) ([]ports.Commit, error) {
			assert.Equal(t, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), since, "commits in the last 7 days")
			if path == "billing/invoice.py" {
				return []ports.Commit{{SHA: "abcdef1234", Author: "dev", Message: "Divide by line count\n\nbody", At: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)}}, nil
			}
			return nil, nil
		},
		Cost: func(_, _, _ string, in, out int64) (float64, bool) { return float64(in+out) / 1e6, true }}
	return d, m, l, sv
}

const good = `{"summary":"Invoices with no lines divide by zero","probable_cause":"render divides by len(lines) since abcdef12",
"impact":"invoice rendering fails","affected_code":[{"chunk_id":"c-frame","reason":"divides by len(lines)"},{"chunk_id":"invented","reason":"x"}],
"next_steps":["guard empty invoices"],"confidence":"high","is_actionable":true,"suggest_known_issue":false}`

// --- tests ---

func TestDecode_AssemblesContextAndStores(t *testing.T) {
	d, m, l, _ := setup(t, good)
	out, err := d.Decode(context.Background(), "iss-1", false, "job-1")
	require.NoError(t, err)
	assert.Equal(t, Outcome{Status: "decoded", DecodeID: "dec-1"}, out)

	p := l.prompts[0]
	for _, want := range []string{"Title: ZeroDivisionError: division by zero", "at billing.invoice render (billing/invoice.py:88)",
		"[c-frame] billing/invoice.py · render", "[c-vec] billing/lines.py", "abcdef12 dev: Divide by line count", "Renders an invoice.",
		"empty invoices divide by zero", "order=[REDACTED]"} {
		assert.Contains(t, p, want)
	}
	assert.Less(t, strings.Index(p, "[c-frame]"), strings.Index(p, "[c-vec]"), "frame matches come before vector matches")
	assert.Equal(t, 1, strings.Count(p, "[c-frame]"), "a chunk found both ways is shown once")
	assert.NotContains(t, p, "Summary: weak", "similar issues below 0.85 are not used")

	require.Len(t, m.saved, 1)
	r := m.saved[0]
	require.Len(t, r.Affected, 1, "an invented chunk ID is dropped")
	assert.Equal(t, Affected{ChunkID: "c-frame", RepoID: "r1", Path: "billing/invoice.py", Symbol: "render", ContentHash: "h1", Reason: "divides by len(lines)"}, r.Affected[0])
	assert.Equal(t, []string{"iss-0"}, r.SimilarIssues, "its own previous decode is not 'similar'")
	require.Len(t, r.RelatedCommits, 1)
	assert.Equal(t, int64(1000), r.Tokens)
	assert.InDelta(t, 0.001, r.CostUSD, 1e-9)
	assert.Equal(t, "anthropic", r.Provider)
	require.Len(t, r.RelatedDocs, 1)

	require.Len(t, m.put, 1, "the decode is indexed for future similar-issue search")
	assert.Equal(t, DecodeChunkID("iss-1"), m.put[0].ID)
	assert.Equal(t, ports.SourceIssueDecode, m.put[0].Source)
	assert.Contains(t, m.put[0].Content, "Invoices with no lines divide by zero")
}

func TestDecode_ReusesWhileAffectedCodeUnchanged(t *testing.T) {
	d, m, l, sv := setup(t, good)
	m.stored = &Stored{ID: "old", FingerprintVersion: signals.FingerprintVersion, Tokens: 5000, CostUSD: 0.05,
		Affected: []Affected{{ChunkID: "c-frame", ContentHash: "h1"}}}
	out, err := d.Decode(context.Background(), "iss-1", false, "")
	require.NoError(t, err)
	assert.Equal(t, Outcome{Status: "reused", DecodeID: "old"}, out)
	assert.Empty(t, l.prompts, "no paid call")
	assert.Equal(t, []string{"decode_reused"}, sv.kinds)

	c := m.chunks["c-frame"]
	c.ContentHash = "h1-changed"
	m.chunks["c-frame"] = c
	out, err = d.Decode(context.Background(), "iss-1", false, "")
	require.NoError(t, err)
	assert.Equal(t, "decoded", out.Status, "the blamed code changed: decode again")

	m.chunks["c-frame"] = ports.Chunk{ID: "c-frame", ContentHash: "h1"}
	m.stored.FingerprintVersion = signals.FingerprintVersion - 1
	d.GW = setupGW(t, good)
	out, err = d.Decode(context.Background(), "iss-1", false, "")
	require.NoError(t, err)
	assert.Equal(t, "decoded", out.Status, "an older fingerprint recipe: decode again")

	m.stored.FingerprintVersion = signals.FingerprintVersion
	d.GW = setupGW(t, good)
	out, err = d.Decode(context.Background(), "iss-1", true, "")
	require.NoError(t, err)
	assert.Equal(t, "decoded", out.Status, "force always decodes")
}

func setupGW(t *testing.T, replies ...string) *llmgateway.Gateway {
	d, _, _, _ := setup(t, replies...)
	return d.GW
}

func TestDecode_LowConfidenceAfterFailedRepair(t *testing.T) {
	d, m, l, _ := setup(t, "The divide in render fails.", "still not json")
	out, err := d.Decode(context.Background(), "iss-1", false, "")
	require.NoError(t, err)
	assert.Equal(t, "decoded", out.Status)
	require.Len(t, l.prompts, 2, "one repair attempt")
	r := m.saved[0].Result
	assert.Equal(t, "low", r.Confidence)
	assert.Equal(t, "still not json", r.Summary, "the model's raw text is kept")
	assert.Equal(t, int64(2000), m.saved[0].Tokens, "both attempts are counted")
}

func TestDecode_SkipsSuppressedAndPacksBudget(t *testing.T) {
	d, m, l, _ := setup(t, good)
	m.issue.Status = "suppressed"
	out, err := d.Decode(context.Background(), "iss-1", false, "")
	require.NoError(t, err)
	assert.Equal(t, "skipped", out.Status)
	assert.Empty(t, l.prompts)

	m.issue.Status = "new"
	c := m.chunks["c-vec"]
	c.Content = strings.Repeat("x", 5000)
	m.chunks["c-vec"] = c
	d.Budget = 800 // 3,200 characters
	_, err = d.Decode(context.Background(), "iss-1", false, "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(l.prompts[0]), 3200)
	assert.NotContains(t, l.prompts[0], "[c-vec]", "a section that does not fit is left out whole")
	assert.NotContains(t, m.saved[0].Affected, Affected{ChunkID: "c-vec"})
}

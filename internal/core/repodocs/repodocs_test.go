package repodocs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
)

func file(p string, lines int, syms ...repodocs.Symbol) repodocs.File {
	for i := range syms {
		syms[i].Path = p
	}
	return repodocs.File{Path: p, Lines: lines, Symbols: syms, Hash: repodocs.Hash(p, fmt.Sprint(lines)), Shape: repodocs.Shape(syms)}
}

func sym(name string, line int, body string) repodocs.Symbol {
	return repodocs.Symbol{Name: name, Kind: "function", Line: line, Lines: strings.Count(body, "\n") + 1, Signature: "func " + name + "()", Body: body}
}

func TestSplit_GroupsDirectoriesWithinBounds(t *testing.T) {
	f := &repodocs.Facts{Files: []repodocs.File{
		file("cmd/app/main.go", 120), file("internal/orders/service.go", 900), file("internal/orders/repo.go", 700),
		file("internal/payments/stripe.go", 1500), file("internal/payments/refund.go", 600), file("internal/util/strings.go", 40),
		file("internal/util/time.go", 30), file("web/src/App.tsx", 400), file("web/src/pages/Orders.tsx", 800),
	}, AllPaths: []string{"internal/orders/service_test.go", "internal/payments/stripe_test.go", "web/src/__tests__/Orders.test.tsx"}}
	mods := repodocs.Split(f, repodocs.SplitOptions{MinLines: 300, MaxLines: 2500, Target: 25})
	byDir := map[string]repodocs.Module{}
	for _, m := range mods {
		byDir[m.Dir] = m
	}
	require.Contains(t, byDir, "internal/orders")
	require.Contains(t, byDir, "internal/payments")
	assert.Equal(t, []string{"internal/orders/service_test.go"}, byDir["internal/orders"].Tests)
	assert.Equal(t, []string{"internal/payments/stripe_test.go"}, byDir["internal/payments"].Tests)
	// The tiny util package (70 lines) does not get a guide of its own.
	assert.NotContains(t, byDir, "internal/util")
	assert.Contains(t, byDir, "web")
	for _, m := range mods {
		assert.NotEmpty(t, m.Key)
		assert.LessOrEqual(t, m.Lines, 2500*2)
	}
	// Every file belongs to exactly one module.
	seen := map[string]int{}
	for _, m := range mods {
		for _, p := range m.Files {
			seen[p]++
		}
	}
	for _, fl := range f.Files {
		assert.Equal(t, 1, seen[fl.Path], fl.Path)
	}
}

func TestCitationsAndWords(t *testing.T) {
	md := "It retries three times [internal/q/retry.go:42] and logs [Dockerfile:3].\n\n| a | b |\n|---|---|\n| x | y |\n\n```go\nfoo()\n```"
	cs := repodocs.Citations(md)
	require.Len(t, cs, 2)
	assert.Equal(t, "internal/q/retry.go", cs[0].Path)
	assert.Equal(t, 42, cs[0].Line)
	assert.Equal(t, 6, repodocs.Words(md), "tables, code and citations are not counted")
}

// fakeDocs answers card requests and document requests like a careful model, citing real lines.
func fakeDocs(t *testing.T, env *fakegw.Env, calls *[]string, mu *sync.Mutex) {
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		user := r.Messages[len(r.Messages)-1].Content
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(r.System, "You take short notes") {
			*calls = append(*calls, "cards")
			var cards []map[string]any
			for _, line := range strings.Split(user, "\n") {
				if strings.HasPrefix(line, "=== ") {
					p := strings.Fields(strings.TrimPrefix(line, "=== "))[0]
					cards = append(cards, map[string]any{"path": p, "purpose": "Handles " + p, "symbols": []any{}, "notes": ""})
				}
			}
			b, _ := json.Marshal(map[string]any{"cards": cards})
			return ports.ChatResponse{Text: string(b)}, nil
		}
		if r.JSONSchema == nil {
			return ports.ChatResponse{Text: "{}"}, nil
		}
		doc := between(user, "Document: ", "\n")
		*calls = append(*calls, doc)
		var secs []map[string]string
		for _, line := range strings.Split(between(user, "Sections (key, title", "\nReply with JSON"), "\n") {
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			key := strings.TrimSuffix(strings.Fields(strings.TrimPrefix(line, "- "))[0], ":")
			if strings.Contains(line, "optional") {
				continue
			}
			md := "The `Refund` function reverses a payment [internal/payments/refund.go:10]; `Charge` takes it [internal/payments/stripe.go:5]."
			secs = append(secs, map[string]string{"key": key, "markdown": md})
		}
		b, _ := json.Marshal(map[string]any{"at_a_glance": "This part moves money. It charges and refunds customers.", "sections": secs, "gaps": []string{}})
		return ports.ChatResponse{Text: string(b), Usage: ports.TokenUsage{InputTokens: 1000, OutputTokens: 300, Reported: true}}, nil
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return s
}

type memStore struct {
	mu    sync.Mutex
	cards map[string]repodocs.Card
	docs  map[string]repodocs.Doc
}

func (m *memStore) Cards(context.Context, string) (map[string]repodocs.Card, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]repodocs.Card{}
	for k, v := range m.cards {
		out[k] = v
	}
	return out, nil
}
func (m *memStore) PutCards(_ context.Context, _ string, cs []repodocs.Card) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range cs {
		m.cards[c.Path] = c
	}
	return nil
}
func (m *memStore) Docs(context.Context, string) ([]repodocs.Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []repodocs.Doc
	for _, d := range m.docs {
		out = append(out, d)
	}
	return out, nil
}
func (m *memStore) PutDoc(_ context.Context, d repodocs.Doc) (repodocs.Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.ID == "" {
		d.ID = d.Type + "/" + d.Key
	}
	m.docs[d.Type+"/"+d.Key] = d
	return d, nil
}
func (m *memStore) DeleteDocsExcept(_ context.Context, _ string, keep [][2]string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ok := map[string]bool{}
	for _, k := range keep {
		ok[k[0]+"/"+k[1]] = true
	}
	var gone []string
	for k := range m.docs {
		if !ok[k] {
			delete(m.docs, k)
			gone = append(gone, k)
		}
	}
	return gone, nil
}

func facts() *repodocs.Facts {
	body := strings.Repeat("\tstep()\n", 20)
	return &repodocs.Facts{RepoID: "r1", Repo: "acme/shop", Head: "abc123", Special: map[string]string{"README.md": "# Shop"},
		Files: []repodocs.File{
			file("internal/payments/stripe.go", 400, sym("Charge", 5, "func Charge() {\n"+body+"}")),
			file("internal/payments/refund.go", 400, sym("Refund", 10, "func Refund() {\n"+body+"}")),
			file("internal/orders/orders.go", 500, sym("Place", 3, "func Place() {\n"+body+"}")),
		},
		Facts: []repodocs.Fact{{Kind: "endpoint", Name: "POST /refunds", Path: "internal/payments/refund.go", Line: 10, From: "Refund"}},
		Calls: []repodocs.Call{{From: "internal/orders/orders.go", To: "internal/payments/stripe.go", N: 2}},
	}
}

func TestRun_WritesDocsAndRewritesOnlyOnRealChange(t *testing.T) {
	env := fakegw.New(fakegw.Options{})
	var calls []string
	var mu sync.Mutex
	fakeDocs(t, env, &calls, &mu)
	st := &memStore{cards: map[string]repodocs.Card{}, docs: map[string]repodocs.Doc{}}
	g := &repodocs.Generator{GW: env.GW, Store: st, Types: []string{"overview", "architecture", "module", "api"}, Rewrite: 0.5}
	ctx := context.Background()
	f := facts()

	res, err := g.Run(ctx, f, repodocs.RunOptions{Meta: llmgateway.CallMeta{RepoID: "r1"}})
	require.NoError(t, err)
	require.Empty(t, res.Failed)
	assert.Equal(t, 3, res.Cards)
	assert.Contains(t, res.Written, "overview/overview")
	assert.Contains(t, res.Written, "architecture/architecture")
	assert.Contains(t, res.Written, "api/api", "the repository exposes an endpoint")
	arch := st.docs["architecture/architecture"]
	comp := arch.Section("components")
	require.NotNil(t, comp)
	assert.True(t, strings.HasPrefix(comp.Markdown, "```mermaid\nflowchart LR"), "the component diagram is drawn from the graph, not by the model")
	for _, d := range st.docs {
		assert.Equal(t, "ok", d.Status, d.Type)
		assert.Greater(t, d.Confidence, 0.6, d.Type+" cites real lines and names")
		assert.Equal(t, "abc123", d.SourceSHA)
	}
	// Module guides are written before the documents that summarize them.
	firstRepoDoc := len(calls)
	for i, c := range calls {
		if c == "Overview" || c == "Architecture" {
			firstRepoDoc = min(firstRepoDoc, i)
		}
	}
	assert.False(t, containsPrefix(calls[firstRepoDoc:], "Module guide"))

	// Nothing changed: nothing is written, not even cards.
	calls = nil
	res, err = g.Run(ctx, f, repodocs.RunOptions{})
	require.NoError(t, err)
	assert.Empty(t, res.Written)
	assert.Empty(t, calls)

	// An edit inside a function body (same structure, under half the lines): still nothing.
	f.Files[2].Hash = "edited"
	res, err = g.Run(ctx, f, repodocs.RunOptions{})
	require.NoError(t, err)
	assert.Empty(t, res.Written)
	assert.InDelta(t, 500.0/1300, st.docs["module/root"].Changed, 0.01, "the change is recorded on the guide")

	// A new declaration changes the structure: that file's card and its module's guide are rewritten.
	calls = nil
	f.Files[1].Symbols = append(f.Files[1].Symbols, sym("PartialRefund", 40, "func PartialRefund() {}"))
	f.Files[1].Shape = repodocs.Shape(f.Files[1].Symbols)
	res, err = g.Run(ctx, f, repodocs.RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Cards)
	assert.True(t, containsPrefix(calls, "Module guide"), calls)
}

func containsPrefix(xs []string, p string) bool {
	for _, x := range xs {
		if strings.HasPrefix(x, p) {
			return true
		}
	}
	return false
}

func moduleKey(st *memStore, dir string) string {
	for _, d := range st.docs {
		if d.Type == "module" && d.Title == dir {
			return d.Key
		}
	}
	return ""
}

func TestRun_ChecksCatchInventedNamesAndBadCitations(t *testing.T) {
	env := fakegw.New(fakegw.Options{})
	attempts := 0
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
		if strings.HasPrefix(r.System, "You take short notes") {
			return ports.ChatResponse{Text: `{"cards":[{"path":"internal/payments/stripe.go","purpose":"p","symbols":[],"notes":""},{"path":"internal/payments/refund.go","purpose":"p","symbols":[],"notes":""},{"path":"internal/orders/orders.go","purpose":"p","symbols":[],"notes":""}]}`}, nil
		}
		attempts++
		md := "Uses `GhostService`, `PhantomRepo`, `FakeCache`, `NopeQueue` [internal/nowhere.go:9] [a/b.go:1] [c/d.go:2]."
		if len(r.Messages) > 1 { // the repair attempt
			md = "Uses `Charge` [internal/payments/stripe.go:5]."
		}
		b, _ := json.Marshal(map[string]any{"at_a_glance": "Moves money.", "gaps": []string{}, "sections": []map[string]string{
			{"key": "what", "markdown": md}, {"key": "concepts", "markdown": md}, {"key": "map", "markdown": "| a | b |"},
			{"key": "entry", "markdown": md}, {"key": "next", "markdown": "Read the architecture."}}})
		return ports.ChatResponse{Text: string(b)}, nil
	}
	st := &memStore{cards: map[string]repodocs.Card{}, docs: map[string]repodocs.Doc{}}
	g := &repodocs.Generator{GW: env.GW, Store: st, Types: []string{"overview"}}
	res, err := g.Run(context.Background(), facts(), repodocs.RunOptions{})
	require.NoError(t, err)
	require.Empty(t, res.Failed)
	assert.Equal(t, 2, attempts, "one repair after the checks failed")
	d := st.docs["overview/overview"]
	assert.NotContains(t, d.Section("what").Markdown, "GhostService")
}

func TestDryRunEstimatesWithoutCalls(t *testing.T) {
	env := fakegw.New(fakegw.Options{})
	called := false
	env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) { called = true; return ports.ChatResponse{}, nil }
	st := &memStore{cards: map[string]repodocs.Card{}, docs: map[string]repodocs.Doc{}}
	g := &repodocs.Generator{GW: env.GW, Store: st}
	res, err := g.Run(context.Background(), facts(), repodocs.RunOptions{DryRun: true})
	require.NoError(t, err)
	assert.False(t, called)
	assert.Contains(t, res.WouldWrite, "overview/overview")
	assert.Greater(t, res.EstimatedTokens, int64(0))
	assert.Empty(t, st.docs)
}

func TestRun_BudgetStopsNewWorkAndIsNeverExceeded(t *testing.T) {
	prices := map[string]spendguard.Price{spendguard.PriceKey("anthropic", "claude-opus-5-5"): {InputPerMTok: 5, OutputPerMTok: 25}}
	types := []string{"overview", "architecture", "module", "api"}
	spent := func(env *fakegw.Env) float64 {
		t, _ := env.Ledger.Spent(context.Background(), ports.SpendFilter{RepoID: "r1"})
		return t.CostUSD
	}
	run := func(b *spendguard.Budget) (*fakegw.Env, *memStore, repodocs.Result, error) {
		env := fakegw.New(fakegw.Options{Prices: prices})
		var calls []string
		var mu sync.Mutex
		fakeDocs(t, env, &calls, &mu)
		reply := env.Model.Handler
		env.Model.Handler = func(r ports.ChatRequest) (ports.ChatResponse, error) {
			time.Sleep(20 * time.Millisecond) // documents are in flight together
			return reply(r)
		}
		st := &memStore{cards: map[string]repodocs.Card{}, docs: map[string]repodocs.Doc{}}
		g := &repodocs.Generator{GW: env.GW, Store: st, Types: types}
		if b != nil {
			b.Spent = func(context.Context) (float64, error) { return spent(env), nil }
		}
		res, err := g.Run(context.Background(), facts(), repodocs.RunOptions{Meta: llmgateway.CallMeta{RepoID: "r1", Budget: b}})
		return env, st, res, err
	}
	env, _, full, err := run(nil)
	require.NoError(t, err)
	total := spent(env)
	require.Positive(t, total)

	// Each call holds its worst case (a full max_output_tokens), far above what these short replies cost,
	// so caps are swept: whatever the cap, the run never passes cap - buffer; a cap that runs out midway
	// stops new work and keeps the documents already finished.
	partial := false
	for capUSD := 0.25; capUSD <= 2; capUSD += 0.25 {
		env, st, res, err := run(&spendguard.Budget{Key: "repo_docs:r1/month", CapUSD: capUSD})
		assert.LessOrEqual(t, spent(env), capUSD-spendguard.BufferUSD(capUSD, spendguard.DefaultBufferPct)+1e-9, "cap $%.2f", capUSD)
		var sb *ports.SpendBlockedError
		if err == nil {
			assert.ElementsMatch(t, full.Written, res.Written)
			continue
		}
		require.ErrorAs(t, err, &sb)
		assert.Equal(t, "repo_docs:r1/month", sb.Scope)
		for _, id := range res.Written {
			assert.Equal(t, "ok", st.docs[id].Status, "documents finished before the stop are kept")
		}
		if len(res.Written) > 0 && len(res.Written) < len(full.Written) {
			partial = true
		}
	}
	assert.True(t, partial, "some cap stopped the run midway")
}

func TestChunksCarryBreadcrumbs(t *testing.T) {
	d := repodocs.Doc{RepoID: "r1", Type: "module", Key: "internal-payments", Title: "internal/payments", Status: "ok", AtAGlance: "Moves money.",
		Sections: []repodocs.DocSection{{Key: "how", Title: "How it works", Markdown: "It charges."}}}
	cs := repodocs.Chunks("acme/shop", d)
	require.Len(t, cs, 2)
	assert.Equal(t, "@docs/module/internal-payments", cs[1].Path)
	assert.True(t, strings.HasPrefix(cs[1].Content, "acme/shop › Module internal/payments › How it works ("), cs[1].Content)
	assert.Contains(t, cs[1].Content, "confidence low")
	assert.Equal(t, "/docs/r/r1/module/internal-payments#how", cs[1].URL)
	assert.Equal(t, ports.SourceGeneratedDoc, cs[1].Source)
}

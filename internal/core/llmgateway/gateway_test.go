package llmgateway

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// --- fakes ---

type memLedger struct {
	mu      sync.Mutex
	records []ports.UsageRecord
}

func (l *memLedger) Spent(ctx context.Context, f ports.SpendFilter) (ports.SpendTotals, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var t ports.SpendTotals
	for _, r := range l.records {
		if (f.Feature == "" || r.Feature == f.Feature) && (f.ProviderID == "" || r.ProviderID == f.ProviderID) && (f.RepoID == "" || r.RepoID == f.RepoID) {
			t.Tokens += r.InputTokens + r.OutputTokens
		}
	}
	return t, nil
}

func (l *memLedger) Record(ctx context.Context, r ports.UsageRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, r)
	return nil
}

type step struct {
	resp ports.ChatResponse
	err  error
}

type fakeLLM struct {
	kind  string
	steps []step
	reqs  []ports.ChatRequest
	// started and gate, when set, hold a call in flight: Chat signals started and waits for gate.
	started chan struct{}
	gate    chan struct{}
}

func (f *fakeLLM) Kind() string { return f.kind }
func (f *fakeLLM) Chat(ctx context.Context, r ports.ChatRequest) (ports.ChatResponse, error) {
	f.reqs = append(f.reqs, r)
	if f.gate != nil {
		f.started <- struct{}{}
		<-f.gate
	}
	if len(f.steps) == 0 {
		return ports.ChatResponse{}, errors.New("no scripted response")
	}
	s := f.steps[0]
	f.steps = f.steps[1:]
	return s.resp, s.err
}
func (f *fakeLLM) Ping(context.Context, string) error { return nil }

type fakeEmb struct {
	batch  int
	calls  [][]string
	report bool
}

func (f *fakeEmb) Kind() string  { return "fake" }
func (f *fakeEmb) MaxBatch() int { return f.batch }
func (f *fakeEmb) Embed(ctx context.Context, model string, texts []string) (ports.EmbedResponse, error) {
	f.calls = append(f.calls, texts)
	r := ports.EmbedResponse{Dimensions: 2}
	for range texts {
		r.Vectors = append(r.Vectors, []float32{1, 2})
	}
	if f.report {
		r.Usage = ports.TokenUsage{InputTokens: int64(len(texts)), Reported: true}
	}
	return r, nil
}

type fakeEngine struct {
	reports bool
	results []step2
	tasks   []contract.DocGenTask
}
type step2 struct {
	res contract.DocGenResult
	err error
}

func (f *fakeEngine) Kind() string       { return "external_cli" }
func (f *fakeEngine) ReportsUsage() bool { return f.reports }
func (f *fakeEngine) Generate(ctx context.Context, t contract.DocGenTask) (contract.DocGenResult, error) {
	f.tasks = append(f.tasks, t)
	s := f.results[0]
	f.results = f.results[1:]
	return s.res, s.err
}

type env struct {
	routes    map[string]Route
	llms      map[string]*fakeLLM
	emb       *fakeEmb
	engine    *fakeEngine
	ledger    *memLedger
	gw        *Gateway
	callsSeen []CallRecord
}

func (e *env) Route(ctx context.Context, f string) (Route, error) {
	r, ok := e.routes[f]
	if !ok {
		return r, ErrNoRoute
	}
	return r, nil
}
func (e *env) LLM(ctx context.Context, id string) (ports.LLM, error) {
	if l, ok := e.llms[id]; ok {
		return l, nil
	}
	return nil, ports.Permanent(errors.New("unknown provider"))
}
func (e *env) Embedder(ctx context.Context, id string) (ports.Embedder, error) { return e.emb, nil }
func (e *env) DocGenerator(ctx context.Context, id string) (ports.DocGenerator, error) {
	return e.engine, nil
}

func newEnv(t *testing.T, limit int64, allowUnreported bool) *env {
	t.Helper()
	e := &env{
		routes: map[string]Route{
			FeatureQA:        {ProviderID: "p1", ProviderKind: "anthropic", Model: "claude-opus-5-5", MaxOutputTokens: 1000, Effort: "medium"},
			FeatureDecode:    {ProviderID: "p1", ProviderKind: "anthropic", Model: "claude-opus-5-5", MaxOutputTokens: 500, Fallback: &Route{ProviderID: "p2", ProviderKind: "openai", Model: "gpt-x", MaxOutputTokens: 500}},
			FeatureEmbedding: {ProviderID: "e1", ProviderKind: "openai", Model: "emb"},
			FeatureDocGen:    {ProviderID: "x1", ProviderKind: "external_cli", Model: "agent", MaxOutputTokens: 100},
		},
		llms:   map[string]*fakeLLM{"p1": {kind: "anthropic"}, "p2": {kind: "openai"}},
		emb:    &fakeEmb{batch: 2, report: true},
		engine: &fakeEngine{reports: true},
		ledger: &memLedger{},
	}
	g, err := spendguard.New([]spendguard.Limit{{ID: "g", Scope: spendguard.ScopeGlobal, Window: spendguard.WindowDay, MaxTokens: limit}}, nil, false)
	require.NoError(t, err)
	e.gw = New(spendguard.NewEnforcer(g, e.ledger, nil), e, e, allowUnreported)
	e.gw.OnCall = func(c CallRecord) { e.callsSeen = append(e.callsSeen, c) }
	return e
}

func ok(text string, in, out int64) step {
	return step{resp: ports.ChatResponse{Text: text, Model: "served-model", Usage: ports.TokenUsage{InputTokens: in, OutputTokens: out, CacheReadTokens: 5, Reported: true}}}
}

func msgs(s string) []ports.ChatMessage { return []ports.ChatMessage{{Role: "user", Content: s}} }

// --- tests ---

func TestChat_AppliesRouteAndRecordsUsage(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{ok("answer", 100, 20)}
	resp, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{RepoID: "r1", UserID: "u1"}, ports.ChatRequest{Messages: msgs("q")})
	require.NoError(t, err)
	assert.Equal(t, "answer", resp.Text)
	sent := e.llms["p1"].reqs[0]
	assert.Equal(t, "claude-opus-5-5", sent.Model)
	assert.Equal(t, 1000, sent.MaxOutputTokens)
	assert.Equal(t, "medium", sent.Effort)
	require.Len(t, e.ledger.records, 1)
	rec := e.ledger.records[0]
	assert.Equal(t, int64(105), rec.InputTokens, "cache reads count as input")
	assert.Equal(t, int64(20), rec.OutputTokens)
	assert.Equal(t, "served-model", rec.Model, "the model that actually served is recorded")
	assert.Equal(t, "r1", rec.RepoID)
	assert.False(t, rec.Estimated)
	assert.Equal(t, "ok", e.callsSeen[0].Outcome)
}

func TestChat_SpendBlockedNeverReachesProvider(t *testing.T) {
	e := newEnv(t, 500, false) // estimate = tiny input + 1000 max output > 500
	_, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	var sb *ports.SpendBlockedError
	require.ErrorAs(t, err, &sb)
	assert.Empty(t, e.llms["p1"].reqs, "no provider call when the ceiling would be breached")
	assert.Empty(t, e.ledger.records)
	assert.Equal(t, "blocked", e.callsSeen[0].Outcome)

	_, err = e.gw.Chat(context.Background(), FeatureQA, CallMeta{Override: true}, ports.ChatRequest{Messages: msgs("q")})
	assert.Error(t, err, "no scripted response — but the override did let the call through to the provider")
	assert.Len(t, e.llms["p1"].reqs, 1)
}

func TestChat_InFlightCallHoldsItsWorstCase(t *testing.T) {
	// 1500 tokens: one call's worst case (tiny input + 1000 max output) fits, two at once do not.
	e := newEnv(t, 1500, false)
	llm := e.llms["p1"]
	llm.started, llm.gate = make(chan struct{}), make(chan struct{})
	llm.steps = []step{ok("first", 100, 20)}
	done := make(chan error)
	go func() {
		_, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
		done <- err
	}()
	<-llm.started
	assert.Equal(t, int64(1001), e.gw.enforcer.Reserved().Tokens)
	_, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	var sb *ports.SpendBlockedError
	require.ErrorAs(t, err, &sb, "the first call's reservation leaves no room")
	assert.Contains(t, sb.Reason, "reserved by calls in flight")
	close(llm.gate)
	require.NoError(t, <-done)
	assert.Zero(t, e.gw.enforcer.Reserved().Tokens, "released once recorded")

	// Recorded at its actual 125 tokens, the first call leaves room for another.
	llm.gate = nil
	llm.steps = []step{ok("second", 100, 20)}
	_, err = e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	require.NoError(t, err)

	// A failed call releases its reservation too.
	llm.steps = []step{{err: errors.New("boom")}}
	_, err = e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	require.Error(t, err)
	assert.Zero(t, e.gw.enforcer.Reserved().Tokens)
}

func TestChat_BudgetFromMeta(t *testing.T) {
	e := newEnv(t, 1e6, false)
	g, err := spendguard.New([]spendguard.Limit{{ID: "g", Scope: spendguard.ScopeGlobal, Window: spendguard.WindowDay, MaxTokens: 1e6}},
		map[string]spendguard.Price{spendguard.PriceKey("anthropic", "claude-opus-5-5"): {InputPerMTok: 5, OutputPerMTok: 25}}, false)
	require.NoError(t, err)
	e.gw.enforcer.SetGuard(g)
	// 1000 max output at $25/MTok is $0.025 worst case; $0.95 spent of a $1 budget (less $0.05) leaves none.
	b := &spendguard.Budget{Key: "repo_docs:r1/month", CapUSD: 1, Spent: func(context.Context) (float64, error) { return 0.95, nil }}
	_, err = e.gw.Chat(context.Background(), FeatureQA, CallMeta{RepoID: "r1", Budget: b}, ports.ChatRequest{Messages: msgs("q")})
	var sb *ports.SpendBlockedError
	require.ErrorAs(t, err, &sb)
	assert.Equal(t, "repo_docs:r1/month", sb.Scope)
	assert.Empty(t, e.llms["p1"].reqs)
}

func TestChat_EstimatesWhenUsageUnreported(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{{resp: ports.ChatResponse{Text: "12345678"}}}
	_, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{System: "abcd", Messages: msgs("efgh")})
	require.NoError(t, err)
	rec := e.ledger.records[0]
	assert.True(t, rec.Estimated)
	assert.Equal(t, int64(2), rec.InputTokens)
	assert.Equal(t, int64(2), rec.OutputTokens)
	assert.Equal(t, "claude-opus-5-5", rec.Model)
}

func TestChat_FallbackOnTransientOnly(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{{err: ports.Transient(errors.New("overloaded"))}}
	e.llms["p2"].steps = []step{ok("from fallback", 1, 1)}
	resp, err := e.gw.Chat(context.Background(), FeatureDecode, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	require.NoError(t, err)
	assert.Equal(t, "from fallback", resp.Text)
	assert.Equal(t, "gpt-x", e.llms["p2"].reqs[0].Model)
	outcomes := []string{e.ledger.records[0].Outcome, e.ledger.records[1].Outcome}
	assert.Equal(t, []string{"error", "ok"}, outcomes)

	e.llms["p1"].steps = []step{{err: ports.Permanent(errors.New("bad request"))}}
	_, err = e.gw.Chat(context.Background(), FeatureDecode, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	assert.Error(t, err)
	assert.Len(t, e.llms["p2"].reqs, 1, "permanent errors do not fall back")

	_, err = e.gw.Chat(context.Background(), FeatureTriage, CallMeta{}, ports.ChatRequest{})
	assert.ErrorIs(t, err, ErrNoRoute)
}

func TestChat_RefusalIsPermanentButRecorded(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{{resp: ports.ChatResponse{Usage: ports.TokenUsage{InputTokens: 50, Reported: true}}, err: &ports.RefusalError{Category: "cyber"}}}
	_, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	var pe *ports.PermanentError
	var re *ports.RefusalError
	require.ErrorAs(t, err, &pe)
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "ok", e.ledger.records[0].Outcome, "a refusal is a billed, completed call")
	assert.Equal(t, int64(50), e.ledger.records[0].InputTokens)
}

type out struct {
	Cosmetic  bool   `json:"cosmetic"`
	Confident bool   `json:"confident"`
	Reason    string `json:"reason"`
}

func TestChatJSON_ValidFirstTime(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{ok("```json\n{\"cosmetic\":true,\"confident\":true,\"reason\":\"comment\"}\n```", 1, 1)}
	var o out
	require.NoError(t, e.gw.ChatJSON(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")}, contract.TriageSchema, &o, nil))
	assert.True(t, o.Cosmetic)
	assert.Equal(t, contract.TriageSchema, e.llms["p1"].reqs[0].JSONSchema, "structured output is requested natively")
}

func TestChatJSON_RepairsOnceWithProblems(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{ok(`{"cosmetic":"yes"}`, 1, 1), ok(`{"cosmetic":false,"confident":true,"reason":"logic"}`, 1, 1)}
	var o out
	require.NoError(t, e.gw.ChatJSON(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")}, contract.TriageSchema, &o, nil))
	assert.Equal(t, "logic", o.Reason)
	repair := e.llms["p1"].reqs[1].Messages
	require.Len(t, repair, 3, "the repair appends the bad reply and the problems (append-only history)")
	assert.Equal(t, "assistant", repair[1].Role)
	assert.Contains(t, repair[2].Content, "$.cosmetic: expected boolean")
	assert.Len(t, e.ledger.records, 2, "both calls are recorded")
}

func TestChatJSON_SecondFailureIsPermanentSchemaError(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{ok(`{}`, 1, 1), ok(`{"cosmetic":true}`, 1, 1)}
	var o out
	err := e.gw.ChatJSON(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")}, contract.TriageSchema, &o, nil)
	var se *ports.SchemaError
	var pe *ports.PermanentError
	require.ErrorAs(t, err, &se)
	require.ErrorAs(t, err, &pe)
}

func TestChatJSON_CheckCallbackAndTruncationRetry(t *testing.T) {
	e := newEnv(t, 1e9, false)
	e.llms["p1"].steps = []step{
		{resp: ports.ChatResponse{Text: `{"cosm`, Usage: ports.TokenUsage{Reported: true}}, err: &ports.TruncatedError{MaxOutputTokens: 1000}},
		ok(`{"cosmetic":true,"confident":true,"reason":""}`, 1, 1),
		ok(`{"cosmetic":true,"confident":true,"reason":"now explained"}`, 1, 1),
	}
	var o out
	check := func() []string {
		if o.Reason == "" {
			return []string{"reason must not be empty"}
		}
		return nil
	}
	require.NoError(t, e.gw.ChatJSON(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")}, contract.TriageSchema, &o, check))
	assert.Equal(t, 2000, e.llms["p1"].reqs[1].MaxOutputTokens, "truncated JSON retries with double the budget")
	assert.Contains(t, e.llms["p1"].reqs[2].Messages[2].Content, "reason must not be empty")
	assert.Equal(t, "now explained", o.Reason)
}

func TestEmbed_BatchesGuardsAndRecords(t *testing.T) {
	e := newEnv(t, 1e6, false)
	vecs, route, err := e.gw.Embed(context.Background(), CallMeta{RepoID: "r"}, []string{"a", "b", "c", "d", "e"})
	require.NoError(t, err)
	assert.Len(t, vecs, 5)
	assert.Equal(t, "emb", route.Model)
	assert.Equal(t, [][]string{{"a", "b"}, {"c", "d"}, {"e"}}, e.emb.calls)
	assert.Len(t, e.ledger.records, 3)
	assert.Equal(t, FeatureEmbedding, e.ledger.records[0].Feature)

	e.emb.report = false
	_, _, err = e.gw.Embed(context.Background(), CallMeta{}, []string{"abcdefgh"})
	require.NoError(t, err)
	assert.True(t, e.ledger.records[3].Estimated)

	small := newEnv(t, 1, false)
	_, _, err = small.gw.Embed(context.Background(), CallMeta{}, []string{"a long text that is over one token"})
	var sb *ports.SpendBlockedError
	assert.ErrorAs(t, err, &sb)
	assert.Empty(t, small.emb.calls)
}

func TestGenerateDocs_UsageRepairAndUnguardedRule(t *testing.T) {
	e := newEnv(t, 1e6, false)
	good := contract.DocGenResult{Status: "success", Usage: &contract.ReportedUsage{InputTokens: 10, OutputTokens: 5, Model: "agent-model"}}
	e.engine.results = []step2{{err: &ports.SchemaError{Problems: []string{"$.docs: bad"}}}, {res: good}}
	task := contract.DocGenTask{JobID: "j", Context: "ctx", ChunksToGenerate: make([]contract.ChunkToGenerate, 3)}
	res, err := e.gw.GenerateDocs(context.Background(), CallMeta{JobID: "j"}, task)
	require.NoError(t, err)
	assert.Equal(t, "success", res.Status)
	require.Len(t, e.engine.tasks, 2)
	assert.Equal(t, []string{"$.docs: bad"}, e.engine.tasks[1].RepairErrors)
	assert.Equal(t, 100, e.engine.tasks[0].MaxOutputTokensPerChunk)
	assert.True(t, e.ledger.records[0].Estimated, "the failed attempt reported nothing: estimate recorded")
	assert.Equal(t, int64(300), e.ledger.records[0].OutputTokens, "estimate = per-chunk max × chunks")
	assert.Equal(t, "agent-model", e.ledger.records[1].Model)

	e.engine.results = []step2{{err: &ports.SchemaError{}}, {err: &ports.SchemaError{Problems: []string{"x"}}}}
	_, err = e.gw.GenerateDocs(context.Background(), CallMeta{}, task)
	var pe *ports.PermanentError
	assert.ErrorAs(t, err, &pe)

	u := newEnv(t, 1e6, false)
	u.engine.reports = false
	_, err = u.gw.GenerateDocs(context.Background(), CallMeta{}, task)
	assert.ErrorContains(t, err, "allow_unreported_usage")
	assert.Empty(t, u.engine.tasks, "an unguarded engine never runs without acknowledgement")

	a := newEnv(t, 1e6, true)
	a.engine.reports = false
	a.engine.results = []step2{{res: contract.DocGenResult{Status: "success"}}}
	_, err = a.gw.GenerateDocs(context.Background(), CallMeta{}, task)
	require.NoError(t, err)
	assert.True(t, a.ledger.records[0].Estimated)
}

func TestProtect_ScrubsAlwaysRedactsPIIPerProvider(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.llms["p1"].steps = []step{ok("a", 1, 1), ok("b", 1, 1)}
	in := msgs("key AKIAIOSFODNN7EXAMPLE failed for jane@example.com")
	_, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: in})
	require.NoError(t, err)
	assert.Equal(t, "key <AWS_KEY> failed for jane@example.com", e.llms["p1"].reqs[0].Messages[0].Content,
		"credentials never reach a provider; personal data does unless the provider asks for redaction")
	assert.Contains(t, in[0].Content, "AKIA", "the caller's messages are not modified")

	r := e.routes[FeatureQA]
	r.RedactPII = true
	e.routes[FeatureQA] = r
	_, err = e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: in})
	require.NoError(t, err)
	assert.Equal(t, "key <AWS_KEY> failed for <EMAIL>", e.llms["p1"].reqs[1].Messages[0].Content)

	_, _, err = e.gw.Embed(context.Background(), CallMeta{}, []string{"password=hunter22 in config"})
	require.NoError(t, err)
	assert.Equal(t, []string{"password=<PASSWORD> in config"}, e.emb.calls[0])
}

func TestChat_RouteWithoutModelUsesTheProviderDefault(t *testing.T) {
	e := newEnv(t, 1e6, false)
	e.routes[FeatureQA] = Route{ProviderID: "p1", ProviderKind: "anthropic", MaxOutputTokens: 1000,
		Fallback: &Route{ProviderID: "p2", ProviderKind: "openai", MaxOutputTokens: 500}}
	e.llms["p1"].steps = []step{ok("answer", 10, 2)}
	_, err := e.gw.Chat(context.Background(), FeatureQA, CallMeta{}, ports.ChatRequest{Messages: msgs("q")})
	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-5-5", e.llms["p1"].reqs[0].Model, "an Anthropic route with no model falls back to Haiku")
	r, err := e.gw.Route(context.Background(), FeatureQA)
	require.NoError(t, err)
	assert.Equal(t, "", r.Fallback.Model, "providers without a default are left alone")
}

package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/core/decode"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// TestDecodeEndToEnd: a new error enqueues a decode that blames the code its stack frame points at; a
// regression with unchanged code reuses the decode (no model call) and records the saving.
// signalEnv is a wired Hub with the stub LLM on the given routes.
type signalEnv struct {
	a      *app
	st     *store.Store
	q      *queue.Queue
	stub   *stubllm.Server
	c      *apiClient
	stubID any
}

// addProvider registers an LLM provider through the API and returns its ID.
func (e *signalEnv) addProvider(t *testing.T, kind, baseURL, key string) any {
	t.Helper()
	code, out := e.c.call("POST", "/providers", map[string]any{"kind": kind, "name": "p-" + kind + "-" + ports.NewID()[:8], "base_url": baseURL, "api_key": key})
	require.Equal(t, http.StatusCreated, code, out)
	return out["id"]
}

// setRoute points a feature at a provider and model.
func (e *signalEnv) setRoute(t *testing.T, feature string, providerID any, model string) {
	t.Helper()
	code, out := e.c.call("PUT", "/routes/"+feature, map[string]any{"provider_id": providerID, "model": model})
	require.Equal(t, http.StatusNoContent, code, out)
}

func newSignalEnv(t *testing.T, routes ...string) *signalEnv {
	t.Helper()
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	q := queue.New(st, queue.Options{})
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), q, log, m)
	require.NoError(t, err)
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	require.NoError(t, a.signals.Reload(ctx))
	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes()}))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	_, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	c.csrf = out["csrf_token"].(string)
	stub := stubllm.New()
	t.Cleanup(stub.Close)
	code, out := c.call("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "stub", "base_url": stub.URL + "/v1", "api_key": "sk"})
	require.Equal(t, http.StatusCreated, code, out)
	provID := out["id"]
	for _, f := range routes {
		model := "stub"
		if f == "embedding" {
			model = "stub-embed"
		}
		code, out = c.call("PUT", "/routes/"+f, map[string]any{"provider_id": provID, "model": model})
		require.Equal(t, http.StatusNoContent, code, out)
	}
	return &signalEnv{a: a, st: st, q: q, stub: stub, c: c, stubID: provID}
}

func TestDecodeEndToEnd(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "decode", "embedding")
	a, st, q, stub := env.a, env.st, env.q, env.stub
	// A repository with the code the stack trace points at, mapped to the "billing" service.
	connID, err := a.conns.Create(ctx, store.NewConnector{Type: "github", Name: "gh", Credentials: "x"})
	require.NoError(t, err)
	repoID := ports.NewID()
	_, err = st.Pool.Exec(ctx, `INSERT INTO repos (id, connector_id, full_name, default_branch) VALUES ($1, $2, 'acme/billing', 'main')`, repoID, connID)
	require.NoError(t, err)
	_, err = st.Pool.Exec(ctx, `INSERT INTO service_map (service_name, repo_id) VALUES ('billing', $1)`, repoID)
	require.NoError(t, err)
	body := "def render(lines):\n    return sum(lines) / len(lines)\n"
	frameChunk := ports.Chunk{ID: chunker.ID("acme/billing", "src/billing/invoice.py", "Invoice.render"), RepoID: repoID, Scope: "acme/billing",
		Source: ports.SourceCode, Path: "src/billing/invoice.py", Symbol: "Invoice.render", Language: "python", Content: body, ContentHash: chunker.Hash(body)}
	other := ports.Chunk{ID: chunker.ID("acme/billing", "src/billing/tax.py", "tax"), RepoID: repoID, Scope: "acme/billing",
		Source: ports.SourceCode, Path: "src/billing/tax.py", Symbol: "tax", Language: "python", Content: "def tax(): pass", ContentHash: "t"}
	_, err = a.chunks.Apply(ctx, store.ChunkWrite{Upserts: []ports.Chunk{other, frameChunk}})
	require.NoError(t, err)

	ev := ports.SignalEvent{Source: "sentry", Kind: ports.KindError, ExternalID: "e1", Service: "billing", ExceptionType: "ZeroDivisionError",
		Title: "ZeroDivisionError: division by zero", Message: "division by zero",
		Stack: []ports.StackFrame{{Module: "billing.invoice", Function: "render", File: "/app/src/billing/invoice.py", Line: 2, InApp: true}}}
	require.NoError(t, a.signals.Ingest(ctx, []ports.SignalEvent{ev}))
	require.NoError(t, a.agg.Flush(ctx))
	runJob(t, a, q, ports.JobDecodeIssue)

	var issueID, status, summary, confidence string
	var affected []byte
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT i.id, i.status::text, d.summary, d.confidence::text, d.affected_code
		FROM issues i JOIN decodes d ON d.id = i.decode_id`).Scan(&issueID, &status, &summary, &confidence, &affected))
	assert.Equal(t, "decoded", status)
	assert.Equal(t, "stub decode of ZeroDivisionError: division by zero", summary)
	assert.Equal(t, "high", confidence)
	assert.Contains(t, string(affected), frameChunk.ID, "the frame /app/src/billing/invoice.py:render maps to Invoice.render")
	var n int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE chunk_id = $1 AND source = 'issue_decode'`, decode.DecodeChunkID(issueID)).Scan(&n))
	assert.Equal(t, 1, n, "the decode is searchable")
	var tokens int64
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(input_tokens + output_tokens), 0) FROM usage_events WHERE feature = 'decode'`).Scan(&n, &tokens))
	assert.Equal(t, 1, n, "one spend-guarded decode call, on the ledger")

	// Resolve, then the same error again: a regression with unchanged code reuses the decode.
	_, err = a.signalStore.Resolve(ctx, []string{ev.Fingerprint})
	require.NoError(t, err)
	_, err = st.Pool.Exec(ctx, `UPDATE issues SET status = 'resolved'`)
	require.NoError(t, err)
	ev.ExternalID = "e2"
	require.NoError(t, a.signals.Ingest(ctx, []ports.SignalEvent{ev}))
	require.NoError(t, a.agg.Flush(ctx))
	runJob(t, a, q, ports.JobDecodeIssue)
	assert.Equal(t, 1, stub.Calls(stubllm.Decode), "no second model call")
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM savings_events WHERE kind = 'decode_reused'`).Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT status::text FROM issues`).Scan(&status))
	assert.Equal(t, "regressed", status, "a regression stays visible as such")
}

// TestSuggestionsEndToEnd: recurring health-check noise that the decode calls not actionable becomes an
// auto-suggestion; accepting it enables a rule that suppresses the next occurrence; pasted text proposes a
// rule for the same issue and the dry run counts it.
func TestSuggestionsEndToEnd(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "decode", "suggest")
	a, st, q := env.a, env.st, env.q

	var evs []ports.SignalEvent
	for i := 0; i < 60; i++ {
		evs = append(evs, ports.SignalEvent{Source: "alertmanager", Kind: ports.KindAlert, ExternalID: "hc-" + strings.Repeat("x", i),
			Service: "lb", Environment: "prod", RuleID: "LBHealth", Title: "health check failed on lb-1"})
	}
	require.NoError(t, a.signals.Ingest(ctx, evs))
	require.NoError(t, a.agg.Flush(ctx))
	runJob(t, a, q, ports.JobDecodeIssue)
	var issueID string
	var actionable bool
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT i.id, d.is_actionable FROM issues i JOIN decodes d ON d.id = i.decode_id`).Scan(&issueID, &actionable))
	assert.False(t, actionable, "the decode calls health-check noise not actionable")

	n, err := a.suggest.AutoSuggest(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "younger than 3 days: not yet")
	_, err = st.Pool.Exec(ctx, `UPDATE issues SET first_seen = now() - interval '4 days'`)
	require.NoError(t, err)
	n, err = a.suggest.AutoSuggest(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, err = a.suggest.AutoSuggest(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "a pending suggestion is not proposed twice")

	pending, err := a.suggestions.ListSuggestions(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, []string{issueID}, pending[0].IssueIDs)
	assert.Equal(t, []string{"lb"}, pending[0].ProposedMatch.Services)
	assert.Contains(t, pending[0].Rationale, "60 occurrences")

	var enabled int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM known_issues WHERE enabled`).Scan(&enabled))
	assert.Equal(t, 0, enabled, "nothing is suppressed without a human")
	var ownerID string
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT id FROM users LIMIT 1`).Scan(&ownerID))
	decided, err := a.suggestions.DecideSuggestion(ctx, pending[0].ID, ownerID, true, "LB health checks during deploys", "expected_noise")
	require.NoError(t, err)
	assert.Equal(t, "accepted", decided.Status)
	_, err = a.suggestions.DecideSuggestion(ctx, pending[0].ID, ownerID, false, "", "")
	assert.ErrorIs(t, err, store.ErrAlreadyDecided)

	require.NoError(t, a.signals.Reload(ctx))
	evs[0].ExternalID = "hc-after"
	require.NoError(t, a.signals.Ingest(ctx, evs[:1]))
	require.NoError(t, a.agg.Flush(ctx))
	var suppressed int64
	var status string
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT suppressed_count, status::text FROM issues WHERE id = $1`, issueID).Scan(&suppressed, &status))
	assert.Equal(t, int64(1), suppressed, "the accepted rule suppresses the next occurrence")

	got, err := a.suggest.FromText(ctx, `Runbook: "health check failed" alerts from lb during rolling deploys are expected.`, nil, ownerID)
	require.NoError(t, err)
	assert.Equal(t, "high", got.Confidence)
	require.Len(t, got.Candidates, 1)
	assert.Equal(t, []string{got.Candidates[0].Fingerprint}, got.ProposedMatch.Fingerprints)
	assert.Equal(t, 1, got.WouldMatch)
	assert.Equal(t, []string{issueID}, got.SampleIssueIDs)
	assert.Equal(t, []string{"health check failed"}, got.Terms.Messages, "found by the quoted message (2-letter service names are too ambiguous to match as words)")
}

// TestDecisionGateEndToEnd: with a decide route, confident noise gets a short decode without a full decode
// call; a real defect still gets the full decode.
func TestDecisionGateEndToEnd(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "decode", "decide")
	a, st, q, stub := env.a, env.st, env.q, env.stub
	require.NoError(t, a.signals.Ingest(ctx, []ports.SignalEvent{
		{Source: "alertmanager", Kind: ports.KindAlert, ExternalID: "n1", Service: "lb", RuleID: "LBHealth", Title: "readiness probe failed during rolling deploy"},
		{Source: "sentry", Kind: ports.KindError, ExternalID: "d1", Service: "orders", ExceptionType: "NullPointerException", Title: "NullPointerException in OrderService.refund"},
	}))
	require.NoError(t, a.agg.Flush(ctx))
	runJob(t, a, q, ports.JobDecodeIssue)
	runJob(t, a, q, ports.JobDecodeIssue)
	assert.Equal(t, 2, stub.Calls(stubllm.Decide), "every new issue is asked")
	assert.Equal(t, 1, stub.Calls(stubllm.Decode), "only the defect pays for a full decode")

	var provider string
	var actionable bool
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT d.provider, d.is_actionable FROM issues i JOIN decodes d ON d.id = i.decode_id WHERE i.service = 'lb'`).Scan(&provider, &actionable))
	assert.Equal(t, "decide:openai_compat", provider)
	assert.False(t, actionable)
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT d.provider, d.is_actionable FROM issues i JOIN decodes d ON d.id = i.decode_id WHERE i.service = 'orders'`).Scan(&provider, &actionable))
	assert.Equal(t, "openai_compat", provider)
	assert.True(t, actionable)
	var saved int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM savings_events WHERE kind = 'decision_gate'`).Scan(&saved))
	assert.Equal(t, 1, saved)
}

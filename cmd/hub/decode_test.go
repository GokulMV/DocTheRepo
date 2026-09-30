package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
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
func TestDecodeEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	_, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	c.csrf = out["csrf_token"].(string)
	stub := stubllm.New()
	defer stub.Close()
	code, out := c.call("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "stub", "base_url": stub.URL + "/v1", "api_key": "sk"})
	require.Equal(t, http.StatusCreated, code, out)
	for f, mdl := range map[string]string{"decode": "stub", "embedding": "stub-embed"} {
		code, out = c.call("PUT", "/routes/"+f, map[string]any{"provider_id": out["id"], "model": mdl})
		require.Equal(t, http.StatusNoContent, code, out)
		_, out = c.call("GET", "/providers", nil) // restore out["id"] for the next iteration
		out = map[string]any{"id": out["items"].([]any)[0].(map[string]any)["id"]}
	}

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

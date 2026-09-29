package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/pgvector"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
)

const ownerPW = "correct horse battery staple"

type askEnv struct {
	srv          *httptest.Server
	st           *store.Store
	fg           *fakegw.Env
	repoA, repoB string
	svc          *auth.Service
}

func newAskEnv(t *testing.T) *askEnv {
	t.Helper()
	ctx := context.Background()
	st := storetest.New(t)
	cfg := config.Default().Auth
	cfg.Mode, cfg.AllUsersReadAllRepos = "local", false
	svc := auth.New(st, cfg)
	_, err := svc.BootstrapOwner(ctx, "owner@acme.com", ownerPW)
	require.NoError(t, err)
	hash, _ := auth.HashPassword(ownerPW)
	_, err = st.Pool.Exec(ctx, `INSERT INTO users (id, email, role, password_hash) VALUES ($1, 'viewer@acme.com', 'viewer', $2)`, ports.NewID(), hash)
	require.NoError(t, err)

	cid, repoA := seedRepo(t, st, "acme/payments")
	_, repoB := seedRepoOn(t, st, cid, "acme/web")
	fg := fakegw.New(fakegw.Options{})
	idx := pgvector.New(st.Pool)
	chunks := []ports.Chunk{
		{ID: "a1", RepoID: repoA, Scope: "acme/payments", Source: ports.SourceCode, Path: "refund.go", Symbol: "Refund", Content: "func Refund(order Order) error { // refunds a captured payment via the gateway }", ContentHash: "1"},
		{ID: "a2", RepoID: repoA, Scope: "acme/payments", Source: ports.SourceGeneratedDoc, Path: "docs/generated/refund.go.md", Symbol: "refund", Content: "Refund reverses a captured payment. Refunds are idempotent per order.", ContentHash: "2"},
		{ID: "b1", RepoID: repoB, Scope: "acme/web", Source: ports.SourceCode, Path: "checkout.ts", Symbol: "refundButton", Content: "export function refundButton() { /* calls the refund API */ }", ContentHash: "3"},
	}
	_, err = store.NewChunks(st).Apply(ctx, ports.ChunkWrite{Upserts: chunks})
	require.NoError(t, err)
	_, err = (&pipeline.Indexer{GW: fg.GW, Index: idx}).Embed(ctx, llmgateway.CallMeta{}, chunks)
	require.NoError(t, err)

	qa := store.NewQA(st)
	eng := &rag.Engine{Store: qa, Index: idx, GW: fg.GW, Savings: store.NewSavings(st)}
	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: slog.New(slog.DiscardHandler), Metrics: observability.NewMetrics(), Auth: svc,
		V1: []func(chi.Router){api.AskRoutes(eng, qa, svc)}}))
	t.Cleanup(srv.Close)
	return &askEnv{srv: srv, st: st, fg: fg, repoA: repoA, repoB: repoB, svc: svc}
}

func (e *askEnv) login(t *testing.T, email string) *client {
	c := newClient(t, e.srv.URL)
	code, out, _ := c.do("POST", "/api/v1/auth/local/login", map[string]string{"email": email, "password": ownerPW})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)
	return c
}

func TestAsk_JSONCitationsCacheAndThreads(t *testing.T) {
	e := newAskEnv(t)
	owner := e.login(t, "owner@acme.com")
	code, out, _ := owner.do("POST", "/api/v1/ask", map[string]any{"question": "How do refunds work?"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "The answer is in the first source [1]. A made-up claim .", out["answer"], "the fabricated citation is dropped")
	cits := out["citations"].([]any)
	require.Len(t, cits, 1)
	assert.NotEmpty(t, cits[0].(map[string]any)["path"])
	assert.Equal(t, false, out["cached"])
	threadID := out["thread_id"].(string)
	msgID := out["message_id"].(string)

	code, again, _ := owner.do("POST", "/api/v1/ask", map[string]any{"question": "  how do REFUNDS work? "})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, again["cached"], "normalized repeat hits the answer cache")
	assert.NotEqual(t, threadID, again["thread_id"])

	code, follow, _ := owner.do("POST", "/api/v1/ask", map[string]any{"question": "And partial refunds?", "thread_id": threadID})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, threadID, follow["thread_id"])
	last := e.fg.Model.Reqs[len(e.fg.Model.Reqs)-1]
	assert.Len(t, last.Messages, 3, "follow-ups carry the thread history")

	code, th, _ := owner.do("GET", "/api/v1/threads/"+threadID, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, th["messages"].([]any), 4)
	code, list, _ := owner.do("GET", "/api/v1/threads", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, list["items"].([]any), 2)

	code, _, _ = owner.do("POST", "/api/v1/messages/"+msgID+"/feedback", map[string]any{"value": "up", "comment": "helpful"})
	assert.Equal(t, http.StatusNoContent, code)
	code, _, _ = owner.do("POST", "/api/v1/messages/"+msgID+"/feedback", map[string]any{"value": "meh"})
	assert.Equal(t, http.StatusBadRequest, code)

	viewer := e.login(t, "viewer@acme.com")
	code, _, _ = viewer.do("GET", "/api/v1/threads/"+threadID, nil)
	assert.Equal(t, http.StatusNotFound, code, "threads are private")
	code, _, _ = viewer.do("POST", "/api/v1/messages/"+msgID+"/feedback", map[string]any{"value": "down"})
	assert.Equal(t, http.StatusNotFound, code)

	code, _, _ = owner.do("DELETE", "/api/v1/threads/"+threadID, nil)
	assert.Equal(t, http.StatusNoContent, code)
	code, _, _ = owner.do("GET", "/api/v1/threads/"+threadID, nil)
	assert.Equal(t, http.StatusNotFound, code)

	code, out, _ = owner.do("POST", "/api/v1/ask", map[string]any{"question": "   "})
	assert.Equal(t, "QUESTION_EMPTY", errCode(out))
	code, out, _ = owner.do("POST", "/api/v1/ask", map[string]any{"question": "q", "scope": map[string]any{"include": []string{"slack"}}})
	assert.Equal(t, http.StatusBadRequest, code)
	code, out, _ = owner.do("POST", "/api/v1/ask", map[string]any{"question": "an unanswerable refund question"})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, rag.NotFoundAnswer, out["answer"], "uncited answers are replaced")
}

func TestAsk_ACLIsEnforcedBeforeTheModel(t *testing.T) {
	e := newAskEnv(t)
	viewer := e.login(t, "viewer@acme.com")
	code, out, _ := viewer.do("POST", "/api/v1/ask", map[string]any{"question": "How do refunds work?"})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, rag.NotFoundAnswer, out["answer"], "a viewer with no grants sees nothing")
	before := len(e.fg.Model.Reqs)

	var viewerID string
	require.NoError(t, e.st.Pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = 'viewer@acme.com'`).Scan(&viewerID))
	require.NoError(t, e.svc.SetRepoAccess(context.Background(), viewerID, []string{e.repoB}, "read"))
	code, out, _ = viewer.do("POST", "/api/v1/ask", map[string]any{"question": "How do refunds work?"})
	require.Equal(t, http.StatusOK, code)
	for _, c := range out["citations"].([]any) {
		assert.Equal(t, "acme/web", c.(map[string]any)["repo"])
	}
	prompt := e.fg.Model.Reqs[len(e.fg.Model.Reqs)-1].Messages[0].Content
	assert.NotContains(t, prompt, "acme/payments", "unreadable repos never reach the model")
	assert.Greater(t, len(e.fg.Model.Reqs), before)

	code, out, _ = viewer.do("POST", "/api/v1/ask", map[string]any{"question": "refunds", "scope": map[string]any{"repo_ids": []string{e.repoA}}})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "SCOPE_FORBIDDEN", errCode(out))
}

func TestAsk_SSEAndErrors(t *testing.T) {
	e := newAskEnv(t)
	owner := e.login(t, "owner@acme.com")
	body, _ := json.Marshal(map[string]any{"question": "How do refunds work?", "scope": map[string]any{"include": []string{"docs"}}})
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/ask", bytes.NewReader(body))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(api.CSRFHeader, owner.csrf)
	resp, err := owner.http.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	events := map[string]int{}
	var done map[string]any
	var deltas strings.Builder
	sc := bufio.NewScanner(resp.Body)
	var ev string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			ev = strings.TrimPrefix(line, "event: ")
			events[ev]++
		case strings.HasPrefix(line, "data: "):
			var m map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m))
			if ev == "delta" {
				deltas.WriteString(m["text"].(string))
			}
			if ev == "done" {
				done = m
			}
		}
	}
	assert.Greater(t, events["delta"], 1, "text streams in pieces")
	assert.Equal(t, 1, events["citation"])
	require.NotNil(t, done)
	assert.Contains(t, deltas.String(), "[1]")
	assert.Equal(t, "The answer is in the first source [1]. A made-up claim .", done["answer"], "done carries the authoritative answer")
	cit := done["citations"].([]any)[0].(map[string]any)
	assert.Equal(t, "doc", cit["type"], "include=docs limits sources")

	delete(e.fg.Routes, "qa")
	code, out, _ := owner.do("POST", "/api/v1/ask", map[string]any{"question": "anything"})
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, "NO_LLM_ROUTE", errCode(out))

	anon := newClient(t, e.srv.URL)
	code, _, _ = anon.do("POST", "/api/v1/ask", map[string]any{"question": "x"})
	assert.Equal(t, http.StatusUnauthorized, code)
}

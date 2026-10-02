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
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// TestAskBroadQuestion: "explain me about how this repo works" shares no rare word with the code, and
// asking for every word to match found nothing. Broad questions now draw on the README (and keyword
// search widens to any word), so the answer cites it. No embedding route, as with Claude alone.
func TestAskBroadQuestion(t *testing.T) {
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

	gh := githubmock.New()
	defer gh.Close()
	gh.CreateRepo("acme/shop", "main", map[string]string{
		"README.md": "# Shop\n\nThis repo is the shop service. It takes orders over HTTP and works with the payments API to charge cards.\n",
		"main.go":   "package main\n\nfunc main() { Serve() }\n\n// Serve starts the HTTP server.\nfunc Serve() {}\n",
	})
	llm := stubllm.New()
	defer llm.Close()

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes()}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	code, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)

	code, out = c.call("POST", "/connectors", map[string]any{"type": "github", "name": "GitHub", "credentials": "ghp_x", "mode": "poll",
		"config": map[string]string{"base_url": gh.APIURL(), "bot_login": gh.BotLogin}})
	require.Equal(t, http.StatusCreated, code, out)
	connID := out["id"].(string)
	code, out = c.call("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "stub", "base_url": llm.URL + "/v1", "api_key": "sk"})
	require.Equal(t, http.StatusCreated, code, out)
	code, out = c.call("PUT", "/routes/qa", map[string]any{"provider_id": out["id"], "model": "stub"})
	require.Equal(t, http.StatusOK, code, out)
	code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": "acme/shop"})
	require.Equal(t, http.StatusCreated, code, out)
	repoID := out["id"].(string)
	runJob(t, a, q, ports.JobCodePush)
	code, out = c.call("POST", "/repos/"+repoID+"/import", map[string]any{})
	require.Equal(t, http.StatusAccepted, code, out)
	runJob(t, a, q, ports.JobImportDocs)

	code, out = c.call("POST", "/ask", map[string]any{"question": "explain me about how this repo works"})
	require.Equal(t, http.StatusOK, code, out)
	assert.NotEqual(t, rag.NotFoundAnswer, out["answer"])
	cites, _ := out["citations"].([]any)
	require.NotEmpty(t, cites, "the answer cites the README: %v", out)
	assert.Equal(t, "README.md", cites[0].(map[string]any)["path"])
}

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
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// TestDocsV2: a push indexes the code and queues the repository's documents; they are written per
// repository (overview, architecture, module guides), cited, scored, searchable, priced and exportable.
func TestDocsV2(t *testing.T) {
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
		"main.go":                  "package main\n\nimport \"os\"\n\nfunc main() { Serve(os.Getenv(\"PORT\")) }\n\n// Serve starts the shop.\nfunc Serve(port string) { orders.Place() }\n",
		"orders/orders.go":         "package orders\n\n// Place records an order.\nfunc Place() error { return save() }\n\nfunc save() error { return nil }\n",
		"orders/orders_test.go":    "package orders\n\nfunc TestPlace(t *testing.T) {}\n",
		"internal/secret/s.go":     "package secret\n\n// Key is not for docs.\nfunc Key() string { return \"\" }\n",
		"README.md":                "# Shop\n\nSells things.\n",
		".github/workflows/ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n",
		".dthignore":               "internal/secret/\n"})
	llm := stubllm.New()
	defer llm.Close()

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes()}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	code, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)
	code, out = c.call("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "stub", "base_url": llm.URL + "/v1", "api_key": "sk-test"})
	require.Equal(t, http.StatusCreated, code, out)
	provID := out["id"].(string)
	for _, f := range []string{"docgen", "embedding", "qa"} {
		code, out = c.call("PUT", "/routes/"+f, map[string]any{"provider_id": provID, "model": "stub"})
		require.Equal(t, http.StatusOK, code, out)
	}
	code, out = c.call("POST", "/connectors", map[string]any{"type": "github", "name": "GitHub", "credentials": "ghp_x", "mode": "poll",
		"config": map[string]string{"base_url": gh.APIURL(), "bot_login": gh.BotLogin}})
	require.Equal(t, http.StatusCreated, code, out)
	code, out = c.call("POST", "/repos", map[string]any{"connector_id": out["id"], "full_name": "acme/shop"})
	require.Equal(t, http.StatusCreated, code, out)
	repoID := out["id"].(string)

	// The push indexes the code and opens no docs PR; it queues the documents.
	runJob(t, a, q, ports.JobCodePush)
	assert.Empty(t, gh.PRs("acme/shop"), "Docs v2 keeps documents in the Hub")

	code, out = c.call("POST", "/repos/"+repoID+"/docs/estimate", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.NotZero(t, out["documents"])
	assert.NotZero(t, out["estimated_tokens"])

	runJob(t, a, q, ports.JobRepoDocs)
	code, out = c.call("GET", "/repo-docs?repo_id="+repoID, nil)
	require.Equal(t, http.StatusOK, code, out)
	items := out["items"].([]any)
	types := map[string]map[string]any{}
	for _, it := range items {
		d := it.(map[string]any)
		types[d["type"].(string)] = d
		assert.Equal(t, "ok", d["status"], "%v: %v", d["title"], d["error"])
	}
	for _, want := range []string{"overview", "architecture", "module", "tests", "build"} {
		assert.Contains(t, types, want)
	}
	assert.NotContains(t, types, "api", "no endpoints in this code")
	budget := out["budget"].(map[string]any)
	assert.GreaterOrEqual(t, budget["cap_usd"], 10.0, "a first cap is set from the estimate")

	code, doc := c.call("GET", "/repo-docs/"+types["overview"]["id"].(string), nil)
	require.Equal(t, http.StatusOK, code, doc)
	assert.NotEmpty(t, doc["at_a_glance"])
	secs := doc["sections"].([]any)
	require.NotEmpty(t, secs)
	var all strings.Builder
	for _, s := range secs {
		all.WriteString(s.(map[string]any)["markdown"].(string))
	}
	assert.NotContains(t, all.String(), "secret", ".dthignore keeps internal/secret out")

	// The documents are in Ask's index, one piece per section, and the per-file docs are not.
	cs, err := a.chunks.ForPaths(ctx, repoID, ports.SourceGeneratedDoc, []string{"@docs/overview/overview"})
	require.NoError(t, err)
	assert.NotEmpty(t, cs)

	// Nothing changed: writing again writes nothing.
	code, out = c.call("POST", "/repos/"+repoID+"/docs/write", map[string]any{})
	require.Equal(t, http.StatusAccepted, code, out)
	before := llm.Calls(stubllm.RepoDocs)
	runJob(t, a, q, ports.JobRepoDocs)
	assert.Equal(t, before, llm.Calls(stubllm.RepoDocs), "unchanged documents cost nothing")

	// The cap stops documents once this month's docs spend reaches it.
	code, _ = c.call("PUT", "/repos/"+repoID+"/docs/budget", map[string]any{"cap_usd": 5})
	require.Equal(t, http.StatusOK, code)
	_, err = st.Pool.Exec(ctx, `INSERT INTO usage_events (id, feature, provider_kind, model, cost_usd, repo_id) VALUES (gen_random_uuid(), 'docgen', 'openai_compat', 'stub', 6, $1)`, repoID)
	require.NoError(t, err)
	code, out = c.call("POST", "/repos/"+repoID+"/docs/write", map[string]any{"full": true})
	require.Equal(t, http.StatusAccepted, code, out)
	jobID := runJob(t, a, q, ports.JobRepoDocs)
	_, job := c.call("GET", "/jobs/"+jobID, nil)
	assert.Equal(t, "spend_blocked", job["status"], job)

	// Export opens a pull request with the documents as Markdown.
	code, out = c.call("POST", "/repos/"+repoID+"/docs/export", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.NotZero(t, out["number"])
	prs := gh.PRs("acme/shop")
	require.Len(t, prs, 1)
	overview, ok := gh.File("acme/shop", prs[0].Head, "docs/generated/overview.md")
	require.True(t, ok)
	assert.Contains(t, overview, "# Overview")
	assert.Contains(t, overview, "#L", "citations link to the code")
	_, ok = gh.File("acme/shop", prs[0].Head, "docs/generated/README.md")
	assert.True(t, ok)
}

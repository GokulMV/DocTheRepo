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
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// TestSystemDocs: when one tracked repository builds on another, the System architecture appears: the
// link, a diagram drawn from it, and a write-up; history documents come from the commits and decision
// records; answers carry a confidence.
func TestSystemDocs(t *testing.T) {
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
	gh.CreateRepo("acme/lib", "main", map[string]string{
		"go.mod":              "module github.com/acme/lib\n\ngo 1.22\n",
		"money.go":            "package lib\n\n// Cents formats an amount.\nfunc Cents(n int64) string { return \"\" }\n",
		"README.md":           "# lib\n\nShared money helpers.\n",
		"deploy/service.yaml": "apiVersion: v1\nkind: Service\nmetadata:\n  name: ledger\nspec:\n  ports:\n    - port: 8080\n",
	})
	gh.CreateRepo("acme/web", "main", map[string]string{
		"go.mod":                   "module github.com/acme/web\n\ngo 1.22\n\nrequire github.com/acme/lib v1.2.0\n",
		"main.go":                  "package main\n\nimport \"github.com/acme/lib\"\n\nfunc main() { _ = lib.Cents(5) }\n",
		"docs/adr/0001-use-lib.md": "# Use acme/lib for money\n\nWe moved formatting into acme/lib.\n",
		// Wiring in configuration and CI: a host acme/lib serves, and acme/lib's reusable workflow.
		"deploy/app.yaml":          "kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - name: web\n          env:\n            - name: LEDGER_URL\n              value: http://ledger.default.svc.cluster.local:8080\n",
		".github/workflows/ci.yml": "on: push\njobs:\n  money:\n    uses: acme/lib/.github/workflows/money-contract.yml@main\n",
	})
	llm := stubllm.New()
	defer llm.Close()

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes(), DocsV2: true, HasSystem: a.hasSystem}))
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
	connID := out["id"]
	ids := map[string]string{}
	for _, name := range []string{"acme/lib", "acme/web"} {
		code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": name})
		require.Equal(t, http.StatusCreated, code, out)
		ids[name] = out["id"].(string)
	}
	runJob(t, a, q, ports.JobCodePush)
	runJob(t, a, q, ports.JobCodePush)
	runJob(t, a, q, ports.JobRepoDocs)
	runJob(t, a, q, ports.JobRepoDocs)

	// History documents: Recent changes from the commits, Decision records from the ADR.
	_, out = c.call("GET", "/repo-docs?repo_id="+ids["acme/web"], nil)
	types := map[string]bool{}
	for _, it := range out["items"].([]any) {
		types[it.(map[string]any)["type"].(string)] = true
	}
	assert.True(t, types["changes"], "Recent changes")
	assert.True(t, types["decisions"], "Decision records from docs/adr")

	// The System architecture: acme/web builds on acme/lib.
	runJob(t, a, q, ports.JobSystemDocs)
	code, sys := c.call("GET", "/system", nil)
	require.Equal(t, http.StatusOK, code, sys)
	links := sys["links"].([]any)
	require.NotEmpty(t, links)
	byKind := map[string]map[string]any{}
	for _, x := range links {
		byKind[x.(map[string]any)["kind"].(string)] = x.(map[string]any)
	}
	require.Contains(t, byKind, "library", "%v", links)
	l := byKind["library"]
	// Configuration and pipelines link them too.
	require.Contains(t, byKind, "api", "a host acme/lib serves, called from acme/web's deployment: %v", links)
	assert.Equal(t, "ledger.default.svc.cluster.local", byKind["api"]["via"])
	assert.Equal(t, "deploy/app.yaml", byKind["api"]["path"])
	require.Contains(t, byKind, "pipeline", "acme/web's CI uses acme/lib's workflow: %v", links)
	assert.Equal(t, "acme/web", byKind["pipeline"]["from_name"])
	assert.Equal(t, "acme/lib", byKind["pipeline"]["to_name"])
	assert.Equal(t, "acme/web", l["from_name"])
	assert.Equal(t, "acme/lib", l["to_name"])
	assert.Equal(t, "library", l["kind"])
	assert.Contains(t, sys["diagram"], "library: github.com/acme/lib")
	doc, ok := sys["doc"].(map[string]any)
	require.True(t, ok, "the owner reads every repository, so sees the write-up: %v", sys)
	assert.Equal(t, "ok", doc["status"], doc["error"])
	assert.True(t, strings.HasPrefix(doc["sections"].([]any)[1].(map[string]any)["markdown"].(string), "```mermaid"), "the diagram is drawn from the links")
	_, me := c.call("GET", "/me", nil)
	assert.Equal(t, true, me["features"].(map[string]any)["system"])

	// The write-up is searchable in Ask, but only by someone who reads every repository it covers.
	pieces, err := store.NewRepoDocs(st).SystemChunks(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, pieces, "the System architecture is indexed for Ask")
	for _, p := range pieces {
		assert.Len(t, p.RequiresRepos, 2, p.Path)
		assert.Equal(t, ports.SourceGeneratedDoc, p.Source)
	}

	// Nothing changed: writing it again costs nothing.
	before := llm.Calls(stubllm.RepoDocs)
	_, _, err = q.Enqueue(ctx, ports.NewJob{Type: ports.JobSystemDocs, SerialKey: "system", Payload: map[string]any{}})
	require.NoError(t, err)
	runJob(t, a, q, ports.JobSystemDocs)
	assert.Equal(t, before, llm.Calls(stubllm.RepoDocs))

	// Answers carry a confidence.
	code, ans := c.call("POST", "/ask", map[string]any{"question": "How are cents formatted?"})
	require.Equal(t, http.StatusOK, code, ans)
	if cits, _ := ans["citations"].([]any); len(cits) > 0 {
		conf := ans["confidence"].(map[string]any)
		assert.Contains(t, []any{"high", "medium", "low"}, conf["label"])
	}
}

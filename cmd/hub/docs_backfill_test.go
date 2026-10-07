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
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// TestDocsForCodeSyncedBeforeDocgen (Docs v1): a repository tracked before docgen had a route is indexed
// without docs. Routing docgen documents it then, and "Generate docs" does so on request; without a route
// it explains why.
func TestDocsForCodeSyncedBeforeDocgen(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	cfg.Docs.Version = 1
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
		"main.go":              "package main\n\nimport \"os\"\n\nfunc main() { Serve(os.Getenv(\"PORT\")) }\n\n// Serve starts the shop.\nfunc Serve(port string) {}\n",
		"internal/secret/s.go": "package secret\n\n// Key is not for docs.\nfunc Key() string { return \"\" }\n",
		".dthignore":           "# not documented\ninternal/secret/\n"})
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
	// Tracking a repository starts its first sync at once.
	code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": "acme/shop"})
	require.Equal(t, http.StatusCreated, code, out)
	repoID := out["id"].(string)
	assert.NotEmpty(t, out["job_id"], "the first sync is queued when the repository is tracked")
	code, out = c.call("PATCH", "/repos/"+repoID, map[string]any{"push_mode": "direct"})
	require.Equal(t, http.StatusOK, code, out)

	// No docgen route yet: "Generate docs" says what to do, and the first sync indexes without docs.
	code, out = c.call("POST", "/repos/"+repoID+"/generate-docs", nil)
	require.Equal(t, http.StatusBadRequest, code, out)
	assert.Equal(t, "NO_DOCGEN_ROUTE", out["error"].(map[string]any)["code"])
	runJob(t, a, q, ports.JobCodePush)
	code, out = c.call("GET", "/docs/tree", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, out["nodes"], "no docgen route: indexed, not documented")

	// Routing docgen documents what was synced without docs.
	code, out = c.call("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "stub", "base_url": llm.URL + "/v1", "api_key": "sk-test"})
	require.Equal(t, http.StatusCreated, code, out)
	provID := out["id"].(string)
	code, out = c.call("PUT", "/routes/docgen", map[string]any{"provider_id": provID, "model": "stub"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, float64(1), out["docs_queued"])
	docsJob := runJob(t, a, q, ports.JobCodePush)
	// The job reported its progress as it went: every file documented, then the docs written.
	code, out = c.call("GET", "/jobs/"+docsJob, nil)
	require.Equal(t, http.StatusOK, code, out)
	prog, _ := out["progress"].(map[string]any)
	require.NotNil(t, prog, "progress recorded: %v", out)
	assert.Equal(t, "writing", prog["stage"])
	assert.NotZero(t, prog["total"])
	code, out = c.call("GET", "/docs/tree", nil)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, out["nodes"], 1, "the repository now has docs")
	// .dthignore keeps internal/secret out of the docs.
	var files []string
	var walk func(parent string)
	walk = func(parent string) {
		path := "/docs/tree?repo_id=" + repoID
		if parent != "" {
			path += "&parent_id=" + parent
		}
		_, o := c.call("GET", path, nil)
		for _, n := range o["nodes"].([]any) {
			m := n.(map[string]any)
			if m["kind"] == "file" {
				files = append(files, m["path"].(string))
			} else if m["has_children"] == true && m["kind"] == "dir" {
				walk(m["id"].(string))
			}
		}
	}
	walk("")
	require.NotEmpty(t, files)
	for _, f := range files {
		assert.NotContains(t, f, "secret", "ignored by .dthignore")
	}

	// Saving the route again queues nothing (it has docs); "Generate docs" regenerates on request.
	code, out = c.call("PUT", "/routes/docgen", map[string]any{"provider_id": provID, "model": "stub"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, float64(0), out["docs_queued"])
	code, out = c.call("POST", "/repos/"+repoID+"/generate-docs", nil)
	require.Equal(t, http.StatusAccepted, code, out)
	assert.NotEmpty(t, out["job_id"])
	runJob(t, a, q, ports.JobCodePush)
}

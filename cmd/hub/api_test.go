package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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
	"github.com/GokulMV/DocTheRepo/test/mocks/llmmock"
)

type apiClient struct {
	t    *testing.T
	base string
	c    *http.Client
	csrf string
}

func (c *apiClient) call(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set(api.CSRFHeader, c.csrf)
	resp, err := c.c.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestAPIEndToEnd drives the wired application through the M1 API: connect a git host, track a repo,
// add a BYO LLM provider, route features, sync, browse the Tree/Palace/Library, and read analytics.
func TestAPIEndToEnd(t *testing.T) {
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
	require.NoError(t, a.docs.SeedShelves(ctx))
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)

	gh := githubmock.New()
	defer gh.Close()
	gh.CreateRepo("acme/shop", "main", map[string]string{"README.md": "# Shop\n\nSells things.\n",
		"main.go":                   "package main\n\nimport \"os\"\n\nfunc main() { Serve(os.Getenv(\"PORT\")) }\n\nfunc Serve(port string) {}\n",
		"docs/adr/0001-postgres.md": "# Use Postgres\n\n## Decision\n\nPostgres.\n"})
	llm := llmmock.New()
	defer llm.Close()

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Checks: a.readiness(), Git: a.ingest, Auth: a.auth, V1: a.v1Routes()}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	code, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)

	// Connect GitHub and track a repo.
	code, out = c.call("POST", "/connectors", map[string]any{"type": "github", "name": "GitHub", "credentials": "ghp_x", "webhook_secret": "s3cret",
		"mode": "poll", "config": map[string]string{"base_url": gh.APIURL(), "bot_login": gh.BotLogin}})
	require.Equal(t, http.StatusCreated, code, out)
	connID := out["id"].(string)
	assert.Equal(t, "/hooks/github/"+connID, out["webhook_path"])
	code, out = c.call("POST", "/connectors", map[string]any{"type": "myspace", "name": "x"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, out = c.call("POST", "/connectors/"+connID+"/test", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, out["ok"], out)
	code, out = c.call("GET", "/connectors", nil)
	require.Equal(t, http.StatusOK, code)
	conn := out["items"].([]any)[0].(map[string]any)
	assert.Equal(t, true, conn["has_credentials"])
	assert.Nil(t, conn["credentials"], "credentials are write-only")

	code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": "acme/shop"})
	require.Equal(t, http.StatusCreated, code, out)
	repoID := out["id"].(string)
	code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": "acme/x", "docs_path": "../etc/"})
	assert.Equal(t, http.StatusBadRequest, code, "docs_path cannot escape the repo")
	code, out = c.call("PATCH", "/repos/"+repoID, map[string]any{"service_name": "shop", "push_mode": "direct"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "shop", out["service_name"])
	code, _ = c.call("PATCH", "/repos/"+repoID, map[string]any{"push_mode": "yolo"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, out = c.call("GET", "/repos", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 1)

	// Bring your own LLM: provider, test, routes.
	code, out = c.call("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "local", "base_url": llm.URL + "/v1", "api_key": "sk-test"})
	require.Equal(t, http.StatusCreated, code, out)
	provID := out["id"].(string)
	code, out = c.call("POST", "/providers/"+provID+"/test", map[string]any{"model": llmmock.OK})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, out["ok"], out)
	code, out = c.call("GET", "/providers", nil)
	require.Equal(t, http.StatusOK, code)
	p0 := out["items"].([]any)[0].(map[string]any)
	assert.Equal(t, true, p0["has_key"])
	assert.Nil(t, p0["api_key"])
	for _, f := range []string{"qa", "embedding"} {
		code, out = c.call("PUT", "/routes/"+f, map[string]any{"provider_id": provID, "model": llmmock.OK, "effort": "medium"})
		require.Equal(t, http.StatusNoContent, code, out)
	}
	code, _ = c.call("PUT", "/routes/qa", map[string]any{"provider_id": provID, "model": "m", "effort": "extreme"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = c.call("PUT", "/routes/bogus", map[string]any{"provider_id": provID, "model": "m"})
	assert.Equal(t, http.StatusNotFound, code)
	code, out = c.call("GET", "/routes", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 2)
	code, _ = c.call("DELETE", "/providers/"+provID, nil)
	assert.Equal(t, http.StatusConflict, code, "a routed provider cannot be deleted")

	// Dry run, import, and a sync processed by the pipeline (no docgen route: index only).
	code, out = c.call("POST", "/repos/"+repoID+"/dry-run", nil)
	require.Equal(t, http.StatusOK, code, out)
	code, out = c.call("POST", "/repos/"+repoID+"/import", map[string]any{})
	require.Equal(t, http.StatusAccepted, code, out)
	runJob(t, a, q, ports.JobImportDocs)
	code, out = c.call("POST", "/connectors/"+connID+"/sync", nil)
	require.Equal(t, http.StatusAccepted, code, out)
	require.Len(t, out["job_ids"].([]any), 1)
	pushJob := runJob(t, a, q, ports.JobCodePush)

	// Browse: Tree, node, Palace, Library.
	code, out = c.call("GET", "/docs/tree", nil)
	require.Equal(t, http.StatusOK, code)
	roots := out["nodes"].([]any)
	require.Len(t, roots, 1)
	code, out = c.call("GET", "/docs/tree?repo_id="+repoID, nil)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out["nodes"])
	file := findFile(t, c, repoID, out["nodes"].([]any))
	code, out = c.call("GET", "/docs/node/"+file, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, out["markdown"], "#", "node markdown comes from the Tree, no git round trip")
	code, _ = c.call("GET", "/docs/node/nope", nil)
	assert.Equal(t, http.StatusNotFound, code)
	code, out = c.call("GET", "/palace/entities?kind=repo", nil)
	require.Equal(t, http.StatusOK, code)
	ents := out["items"].([]any)
	require.NotEmpty(t, ents)
	code, out = c.call("GET", "/palace/entities/"+ents[0].(map[string]any)["id"].(string)+"/graph?depth=2", nil)
	require.Equal(t, http.StatusOK, code)
	assert.NotEmpty(t, out["nodes"])
	code, _ = c.call("GET", "/palace/entities/"+ents[0].(map[string]any)["id"].(string)+"/graph?depth=9", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	code, out = c.call("GET", "/library/shelves", nil)
	require.Equal(t, http.StatusOK, code)
	assert.NotEmpty(t, out["shelves"])
	code, out = c.call("GET", "/library/shelves/decisions", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 1, "the imported ADR is on the Decisions shelf")
	code, out = c.call("POST", "/library/shelves", map[string]any{"slug": "onboarding", "title": "Onboarding", "curated": true})
	require.Equal(t, http.StatusCreated, code, out)
	shelfID := out["id"].(string)
	code, _ = c.call("POST", "/library/shelves", map[string]any{"slug": "onboarding", "title": "Dup"})
	assert.Equal(t, http.StatusConflict, code)
	code, _ = c.call("POST", "/library/shelves/"+shelfID+"/items", map[string]any{"item_type": "doc_node", "item_id": file, "note": "start here"})
	require.Equal(t, http.StatusNoContent, code)
	code, out = c.call("GET", "/library/shelves/onboarding", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "start here", out["items"].([]any)[0].(map[string]any)["note"])
	code, _ = c.call("PATCH", "/library/shelves/"+shelfID, map[string]any{"title": "Start here"})
	assert.Equal(t, http.StatusNoContent, code)
	code, _ = c.call("DELETE", "/library/shelves/"+shelfID, nil)
	assert.Equal(t, http.StatusNoContent, code)

	// Ask over the imported docs (the mock model cites nothing, so the contract answers "not found").
	code, out = c.call("POST", "/ask", map[string]any{"question": "Which database do we use?"})
	require.Equal(t, http.StatusOK, code, out)
	assert.NotEmpty(t, out["answer"])

	// Jobs, activity, analytics.
	code, out = c.call("GET", "/jobs?type=code_push", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 1)
	code, out = c.call("GET", "/jobs/"+pushJob, nil)
	require.Equal(t, http.StatusOK, code)
	code, out = c.call("POST", "/jobs/"+pushJob+"/retry", nil)
	assert.Equal(t, http.StatusConflict, code, "done jobs are not retried")
	code, _ = c.call("GET", "/jobs?status=exploded", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	code, out = c.call("GET", "/activity", nil)
	require.Equal(t, http.StatusOK, code)
	assert.NotEmpty(t, out["items"])
	code, out = c.call("GET", "/analytics/usage?group_by=feature&granularity=hour", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Greater(t, out["totals"].(map[string]any)["calls"], float64(0))
	code, _ = c.call("GET", "/analytics/usage?group_by=planet", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	code, out = c.call("GET", "/analytics/savings", nil)
	require.Equal(t, http.StatusOK, code)
	code, out = c.call("GET", "/analytics/pipeline", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["freshness"].([]any), 1)
	code, out = c.call("GET", "/analytics/connectors", nil)
	require.Equal(t, http.StatusOK, code)

	// Spend ceilings and reindex.
	code, out = c.call("GET", "/spend/limits", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 1, "the seeded 2M/day global ceiling")
	code, _ = c.call("PUT", "/spend/limits", map[string]any{"items": []map[string]any{{"scope": "global", "window": "day", "max_tokens": 5000000},
		{"scope": "feature", "scope_key": "qa", "window": "month", "max_cost_usd": 50}}})
	require.Equal(t, http.StatusNoContent, code)
	code, out = c.call("GET", "/spend/limits", nil)
	assert.Len(t, out["items"].([]any), 2)
	code, _ = c.call("PUT", "/spend/limits", map[string]any{"items": []map[string]any{{"scope": "galaxy"}}})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = c.call("POST", "/reindex", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, code)
	code, out = c.call("POST", "/reindex", map[string]any{"acknowledge_destructive": true})
	require.Equal(t, http.StatusAccepted, code, out)
	assert.Greater(t, out["chunks_to_reembed"], float64(0))

	code, _ = c.call("PATCH", "/connectors/"+connID, map[string]any{"enabled": false})
	assert.Equal(t, http.StatusNoContent, code)
	code, _ = c.call("DELETE", "/connectors/"+connID, nil)
	assert.Equal(t, http.StatusNoContent, code)

	resp, err := http.Get(srv.URL + "/readyz")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// runJob claims and runs one queued job of type t through the app's handlers.
func runJob(t *testing.T, a *app, q *queue.Queue, typ ports.JobType) string {
	t.Helper()
	ctx := context.Background()
	job, err := q.Claim(ctx, typ, "test-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, job, "a %s job is queued", typ)
	var out ports.Outcome
	switch typ {
	case ports.JobCodePush:
		out, err = a.pipe.CodePush(ctx, *job)
	case ports.JobImportDocs:
		out, err = a.pipe.ImportDocs(ctx, *job)
	}
	require.NoError(t, err)
	status := out.Status
	if status == "" {
		status = ports.JobDone
	}
	require.NoError(t, q.Finish(ctx, job.ID, "test-worker", status, out.Result, out.Message))
	return job.ID
}

func findFile(t *testing.T, c *apiClient, repoID string, nodes []any) string {
	t.Helper()
	for _, n := range nodes {
		m := n.(map[string]any)
		if m["kind"] == "file" {
			return m["id"].(string)
		}
		if m["has_children"] == true {
			_, out := c.call("GET", "/docs/tree?repo_id="+repoID+"&parent_id="+m["id"].(string), nil)
			if id := findFile(t, c, repoID, out["nodes"].([]any)); id != "" {
				return id
			}
		}
	}
	return ""
}

// TestOpenAPICoversEveryRoute fails when a mounted route is missing from api.Operations.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	st := storetest.New(t)
	kek, _ := localfile.Open(filepath.Join(t.TempDir(), "k"))
	cfg := config.Default()
	log := slog.New(slog.DiscardHandler)
	q := queue.New(st, queue.Options{})
	a, err := wire(context.Background(), cfg, st, secrets.NewBox(kek), q, log, observability.NewMetrics())
	require.NoError(t, err)
	h := api.NewRouter(api.Deps{Log: log, Metrics: observability.NewMetrics(), Git: a.ingest, Auth: a.auth, V1: a.v1Routes()})
	documented := map[string]bool{}
	for _, o := range api.Operations {
		documented[o.Method+" "+o.Path] = true
	}
	var missing []string
	require.NoError(t, chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.ReplaceAll(route, "/*", ""), "/")
		route = strings.Replace(route, "{kind:github|gitlab}", "{kind}", 1)
		if route == "" {
			return nil
		}
		if !documented[method+" "+route] {
			missing = append(missing, method+" "+route)
		}
		return nil
	}))
	assert.Empty(t, missing, "add these to api.Operations")
	spec := api.OpenAPI()
	assert.Equal(t, "3.1.0", spec["openapi"])
}

// TestWebhookRegisteredAndDelivered: tracking a repo on a webhook connector registers the hook on the git
// host; a developer push is delivered, verified, and queued; the Hub bot's own push is dropped by the
// bot-loop guard.
func TestWebhookRegisteredAndDelivered(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	cfg.Server.PublicURL = "http://" + l.Addr().String()
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	q := queue.New(st, queue.Options{})
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), q, log, m)
	require.NoError(t, err)
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: api.NewRouter(api.Deps{Log: log, Metrics: m, Git: a.ingest, Auth: a.auth, V1: a.v1Routes()})}}
	srv.Start()
	defer srv.Close()

	gh := githubmock.New()
	defer gh.Close()
	gh.CreateRepo("acme/shop", "main", map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	_, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	c.csrf = out["csrf_token"].(string)
	code, out := c.call("POST", "/connectors", map[string]any{"type": "github", "name": "GitHub", "credentials": "ghp_x", "webhook_secret": "s3cret",
		"mode": "webhook", "config": map[string]string{"base_url": gh.APIURL(), "bot_login": gh.BotLogin}})
	require.Equal(t, http.StatusCreated, code, out)
	connID := out["id"].(string)
	code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": "acme/shop"})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, "registered", out["webhook"])
	hooks := gh.Hooks("acme/shop")
	require.Len(t, hooks, 1)
	assert.Equal(t, srv.URL+"/hooks/github/"+connID, hooks[0]["config"].(map[string]any)["url"])
	code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": "acme/shop"})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Len(t, gh.Hooks("acme/shop"), 1, "re-registering edits the hook instead of adding a second")

	body := "package main\n\nfunc main() { Serve() }\n\nfunc Serve() {}\n"
	dev := gh.Push("acme/shop", "main", map[string]*string{"main.go": &body}, "ann")
	doc := "# generated\n"
	gh.Push("acme/shop", "main", map[string]*string{"docs/generated/main.md": &doc}, gh.BotLogin)
	require.Eventually(t, func() bool { return len(gh.Deliveries()) == 2 }, 10*time.Second, 20*time.Millisecond)
	d := gh.Deliveries()
	assert.Equal(t, http.StatusAccepted, d[0].Status, "developer push: verified and queued")
	assert.Equal(t, dev, d[0].After)
	assert.Equal(t, http.StatusOK, d[1].Status, "the bot's own push is acknowledged but not queued")
	job, err := q.Claim(ctx, ports.JobCodePush, "t", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, job)
	job2, err := q.Claim(ctx, ports.JobCodePush, "t", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, job2, "exactly one push job")
}

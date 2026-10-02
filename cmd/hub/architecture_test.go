package main

import (
	"context"
	"io"
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
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
)

const archifyHTML = `<!DOCTYPE html><html><head><meta charset="UTF-8"><meta name="generator" content="archify 3.0.1">
<title>Shop &amp; Payments Diagram</title></head><body><script>document.body.dataset.ok = "1"</script></body></html>`

// TestArchitectureTab: every tracked repository gets a generated architecture from the knowledge graph;
// archify diagrams committed to it are found by a scan and kept current by pushes; diagrams are served
// sandboxed; and a viewer never sees a repository outside their access.
func TestArchitectureTab(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	cfg.Auth.AllUsersReadAllRepos = false // per-repository access, so the viewer case below means something
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), queue.New(st, queue.Options{}), log, m)
	require.NoError(t, err)
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)

	gh := githubmock.New()
	defer gh.Close()
	gh.CreateRepo("acme/shop", "main", map[string]string{
		"README.md":                          "# Shop\n",
		"docs/architecture/system.html":      archifyHTML,
		"docs/architecture/not-archify.html": "<html><head><title>x</title></head></html>",
		"web/index.html":                     `<meta name="generator" content="archify 3">`, // not a candidate path
	})
	gh.CreateRepo("acme/billing", "main", map[string]string{"README.md": "# Billing\n"})

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
	repoIDs := map[string]string{}
	for _, name := range []string{"acme/shop", "acme/billing"} {
		code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": name})
		require.Equal(t, http.StatusCreated, code, out)
		repoIDs[name] = out["id"].(string)
	}
	shop, billing := repoIDs["acme/shop"], repoIDs["acme/billing"]

	// A small graph, as code extraction would leave it: shop exposes an endpoint, publishes a topic that
	// billing consumes, uses a datastore, and calls billing.
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := st.Pool.Exec(ctx, q, args...)
		require.NoError(t, err)
	}
	ent := func(id, kind, key string, repo any) {
		name := key[strings.LastIndex(key, ":")+1:] // "acme/shop:POST /orders" → "POST /orders"
		exec(`INSERT INTO entities (id, kind, key, name, repo_id) VALUES ($1, $2, $3, $4, $5)`, id, kind, key, name, repo)
	}
	edge := func(src, kind, dst string, repo any) {
		exec(`INSERT INTO edges (src_id, kind, dst_id, repo_id, source_path) VALUES ($1, $2, $3, $4, 'x')`, src, kind, dst, repo)
	}
	id := func(n int) string { return "00000000-0000-0000-0000-00000000000" + string(rune('0'+n)) }
	ent(id(1), "repo", "acme/shop", shop)
	ent(id(2), "service", "shop", nil)
	ent(id(3), "endpoint", "acme/shop:POST /orders", shop)
	ent(id(4), "symbol", "acme/shop:orders.go#Create", shop)
	ent(id(5), "queue_topic", "orders.created", nil)
	ent(id(6), "datastore", "postgres-shop", nil)
	ent(id(7), "symbol", "acme/billing:consumer.go#OnOrder", billing)
	ent(id(8), "endpoint", "acme/billing:POST /invoices", billing)
	edge(id(2), "deployed_as", id(1), shop)
	edge(id(4), "publishes", id(5), shop)
	edge(id(4), "uses_datastore", id(6), shop)
	edge(id(4), "calls", id(8), shop)
	edge(id(7), "subscribes", id(5), billing)

	code, out = c.call("GET", "/architecture", nil)
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, out["items"], 2)
	first := out["items"].([]any)[1].(map[string]any) // ordered by name: billing, shop
	assert.Equal(t, "acme/shop", first["full_name"])
	assert.EqualValues(t, 1, first["endpoints"])
	assert.EqualValues(t, 1, first["topics"])

	code, out = c.call("GET", "/architecture/repos/"+shop, nil)
	require.Equal(t, http.StatusOK, code, out)
	layers := map[string]string{}
	for _, n := range out["nodes"].([]any) {
		n := n.(map[string]any)
		layers[n["name"].(string)] = n["layer"].(string)
	}
	assert.Equal(t, map[string]string{"shop": "core", "/orders": "interface", "orders.created": "messaging",
		"postgres-shop": "data", "acme/billing": "downstream"}, layers)
	assert.Empty(t, out["diagrams"], "nothing scanned yet")

	// Scan: one archify diagram found; the plain HTML and the non-candidate path are ignored.
	code, out = c.call("POST", "/architecture/repos/"+shop+"/scan", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.EqualValues(t, 1, out["found"])
	assert.EqualValues(t, 2, out["checked"])
	_, out = c.call("GET", "/architecture/repos/"+shop, nil)
	diagrams := out["diagrams"].([]any)
	require.Len(t, diagrams, 1)
	d := diagrams[0].(map[string]any)
	assert.Equal(t, "docs/architecture/system.html", d["path"])
	assert.Equal(t, "Shop & Payments", d["title"])
	assert.Equal(t, "archify 3.0.1", d["generator"])

	// Served sandboxed, frameable only by the Hub itself.
	resp, err := c.c.Get(srv.URL + "/api/v1/architecture/diagrams/" + d["id"].(string))
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, archifyHTML, string(body))
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Security-Policy"), "sandbox allow-scripts"))
	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "connect-src 'none'")
	assert.NotContains(t, resp.Header.Get("Content-Security-Policy"), "allow-same-origin")
	assert.Equal(t, "SAMEORIGIN", resp.Header.Get("X-Frame-Options"))

	// A push that deletes the diagram removes it; the pipeline hook does this on every code push.
	rc, err := a.repos.Get(ctx, shop)
	require.NoError(t, err)
	host, err := a.hosts.Host(ctx, connID)
	require.NoError(t, err)
	head := gh.Push("acme/shop", "main", map[string]*string{"docs/architecture/system.html": nil}, "dev")
	a.archSync.OnChanges(ctx, rc, host, head, []ports.ChangedFile{{Path: "docs/architecture/system.html", Status: ports.FileRemoved}})
	_, out = c.call("GET", "/architecture/repos/"+shop, nil)
	assert.Empty(t, out["diagrams"])
	// And one that adds a diagram stores it.
	html := strings.Replace(archifyHTML, "Shop &amp; Payments", "Checkout flow", 1)
	head = gh.Push("acme/shop", "main", map[string]*string{"design/checkout.html": &html}, "dev")
	a.archSync.OnChanges(ctx, rc, host, head, []ports.ChangedFile{{Path: "design/checkout.html", Status: ports.FileAdded}})
	_, out = c.call("GET", "/architecture/repos/"+shop, nil)
	require.Len(t, out["diagrams"], 1)
	assert.Equal(t, "Checkout flow", out["diagrams"].([]any)[0].(map[string]any)["title"])

	// A viewer with access to shop only: billing is hidden everywhere, including as shop's dependency.
	viewer, err := a.auth.UpsertOIDCUser(ctx, auth.Claims{Subject: "v1", Email: "viewer@acme.com", Name: "Vee"})
	require.NoError(t, err)
	require.NoError(t, a.auth.SetRepoAccess(ctx, viewer.ID, []string{shop}, "read"))
	tok, _, err := a.auth.CreateToken(ctx, viewer.ID, "t", 0)
	require.NoError(t, err)
	get := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1"+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	code, body2 := get("/architecture")
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body2, "acme/billing")
	code, _ = get("/architecture/repos/" + billing)
	assert.Equal(t, http.StatusNotFound, code)
	code, body2 = get("/architecture/repos/" + shop)
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body2, "acme/billing", "the hidden dependency is left out")
	assert.Contains(t, body2, `"restricted":2`, "the call to billing and billing consuming the topic")
	code, _ = c.call("POST", "/architecture/repos/"+billing+"/scan", nil)
	assert.Equal(t, http.StatusOK, code, "the owner may scan any repository")
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/architecture/repos/"+shop+"/scan", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "scanning needs editor")
}

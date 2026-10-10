package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/mcpconn"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// fakeGitHubHost is a GitHub Enterprise Server-shaped fake (the Hub reaches it through base_url): the App
// manifest conversion, the OAuth web flow's token endpoint (errors are HTTP 200 with "error"), and a
// GitHub MCP server that takes the user token it issued.
type fakeGitHubHost struct {
	mu        sync.Mutex
	challenge string
	token     string
	forms     []url.Values
	noClient  bool // an older GitHub answer without client_id/client_secret
}

func (g *fakeGitHubHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	answer := func(code int, v any) { w.WriteHeader(code); _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/app-manifests/mcode/conversions":
		app := map[string]any{"id": 42, "slug": "docthrepo-acme", "pem": "-----BEGIN RSA PRIVATE KEY-----\nx\n-----END RSA PRIVATE KEY-----",
			"webhook_secret": "whsec", "owner": map[string]any{"login": "acme"}}
		if !g.noClient {
			app["client_id"], app["client_secret"] = "Iv23liMANIFEST", "manifest-secret"
		}
		answer(http.StatusCreated, app)
	case r.URL.Path == "/login/oauth/access_token":
		_ = r.ParseForm()
		g.forms = append(g.forms, r.PostForm)
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("client_id") != "Iv23liMANIFEST" || r.PostForm.Get("client_secret") != "manifest-secret" {
			answer(http.StatusOK, map[string]any{"error": "incorrect_client_credentials"})
			return
		}
		if r.PostForm.Get("code") != "gh-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			answer(http.StatusOK, map[string]any{"error": "bad_verification_code"})
			return
		}
		g.token = "ghu_user"
		answer(http.StatusOK, map[string]any{"access_token": g.token, "expires_in": 28800, "refresh_token": "ghr_user", "refresh_token_expires_in": 15897600, "token_type": "bearer", "scope": ""})
	case r.URL.Path == "/mcp/":
		if g.token == "" || r.Header.Get("Authorization") != "Bearer "+g.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var msg struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &msg)
		if msg.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "github"}}
		if msg.Method == "tools/list" {
			result = map[string]any{"tools": []any{map[string]any{"name": "list_pull_requests", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}}
		}
		answer(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	default:
		http.NotFound(w, r)
	}
}

const hubPublic = "https://hub.example"

type githubMCPEnv struct {
	st    *store.Store
	svc   *auth.Service
	srv   *httptest.Server
	gh    *fakeGitHubHost
	ghURL string
	keys  *store.SealKeys
	conns *store.Connectors
	owner *client
}

func newGitHubMCPEnv(t *testing.T) *githubMCPEnv {
	t.Helper()
	e := &githubMCPEnv{gh: &fakeGitHubHost{}}
	ghSrv := httptest.NewServer(e.gh)
	t.Cleanup(ghSrv.Close)
	e.ghURL = ghSrv.URL
	e.st = storetest.New(t)
	cfg := config.Default().Auth
	cfg.Mode, cfg.AllUsersReadAllRepos = "local", true
	e.svc = auth.New(e.st, cfg)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	box := secrets.NewBox(kek)
	e.conns = store.NewConnectors(e.st, box, secrets.ConnectorCredsAAD, secrets.ConnectorWebhookAAD)
	e.keys = store.NewSealKeys(e.st, box)
	mcpStore := store.NewMCPServers(e.st, box, secrets.MCPSecretAAD, secrets.MCPOAuthAAD)
	mgr := &mcpconn.Manager{Store: mcpStore, GitHubApps: e.conns, PublicURL: hubPublic}
	d := api.Deps{Log: slog.New(slog.DiscardHandler), Metrics: observability.NewMetrics(), Auth: e.svc, V1: []func(chi.Router){
		api.GitHubConnectRoutes(api.GitHubConnectDeps{Auth: e.svc, Connectors: e.conns, Seal: box.Seal, Open: box.Open, PublicURL: hubPublic, SealKeys: e.keys}),
		api.MCPRoutes(api.MCPDeps{Auth: e.svc, Store: mcpStore, Manager: mgr, SealKeys: e.keys}),
	}}
	e.srv = httptest.NewServer(api.NewRouter(d))
	t.Cleanup(e.srv.Close)
	_, err = e.svc.BootstrapOwner(context.Background(), "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	e.owner = newClient(t, e.srv.URL)
	code, out, _ := e.owner.do("POST", "/api/v1/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	e.owner.csrf = out["csrf_token"].(string)
	return e
}

// seal seals v to the Hub's key for purpose, as the web UI does.
func (e *githubMCPEnv) seal(t *testing.T, v, purpose string) string {
	t.Helper()
	k, err := e.keys.Active(context.Background())
	require.NoError(t, err)
	s, err := secrets.Seal(k.Public(), []byte(v), purpose)
	require.NoError(t, err)
	return s
}

func githubApps(t *testing.T, c *client) []any {
	t.Helper()
	code, out, _ := c.do("GET", "/api/v1/mcp/servers", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, hubPublic+mcpconn.CallbackPath, out["redirect_uri"])
	return out["github_apps"].([]any)
}

// The one-click App asks GitHub for the MCP callback URL; GitHub's conversion returns the App's OAuth
// client, which the Hub keeps (sealed); the admin then signs in to the GitHub MCP server on GitHub's page.
func TestGitHubMCP_SignInWithTheHubsGitHubApp(t *testing.T) {
	e := newGitHubMCPEnv(t)
	require.Empty(t, githubApps(t, e.owner), "no App yet: the UI shows the token form")

	code, out, _ := e.owner.do("POST", "/api/v1/github/connect", map[string]any{"base_url": e.ghURL})
	require.Equal(t, http.StatusOK, code, out)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal([]byte(out["manifest"].(string)), &manifest))
	assert.Equal(t, []any{hubPublic + "/api/v1/mcp/oauth/callback"}, manifest["callback_urls"], "GitHub only returns to a registered callback URL")
	assert.Equal(t, false, manifest["request_oauth_on_install"])
	perms := manifest["default_permissions"].(map[string]any)
	assert.Equal(t, "read", perms["issues"])
	assert.Equal(t, "read", perms["actions"])
	assert.Equal(t, "write", perms["contents"], "the Hub's own permissions are unchanged")
	action, _ := url.Parse(out["action"].(string))
	state := action.Query().Get("state")

	// GitHub created the App and sends the browser back with a code.
	code, _, h := e.owner.do("GET", "/api/v1/github/connect/callback?"+url.Values{"state": {state}, "code": {"mcode"}}.Encode(), nil)
	require.Equal(t, http.StatusFound, code)
	require.Contains(t, h.Get("Location"), "/installations/new", h.Get("Location"))
	apps := githubApps(t, e.owner)
	require.Len(t, apps, 1)
	app := apps[0].(map[string]any)
	assert.Equal(t, true, app["oauth"])
	assert.Equal(t, "Iv23liMANIFEST", app["client_id"])
	assert.Equal(t, e.ghURL, app["web"])
	assert.NotContains(t, app, "client_secret")
	var raw []byte
	require.NoError(t, e.st.Pool.QueryRow(context.Background(), `SELECT oauth_client_secret_ct FROM connectors WHERE id = $1`, app["connector_id"]).Scan(&raw))
	assert.NotContains(t, string(raw), "manifest-secret", "the client secret is stored sealed")

	// github_app needs OAuth and a real App.
	code, out, _ = e.owner.do("POST", "/api/v1/mcp/servers", map[string]any{"name": "GitHub", "url": e.ghURL + "/mcp/", "auth": "bearer",
		"config": map[string]string{"github_app": app["connector_id"].(string)}})
	assert.Equal(t, http.StatusBadRequest, code, out)
	code, out, _ = e.owner.do("POST", "/api/v1/mcp/servers", map[string]any{"name": "GitHub", "url": e.ghURL + "/mcp/", "auth": "oauth",
		"config": map[string]string{"github_app": "00000000-0000-0000-0000-000000000000"}})
	assert.Equal(t, http.StatusBadRequest, code, out)

	code, out, _ = e.owner.do("POST", "/api/v1/mcp/servers", map[string]any{"name": "GitHub", "url": e.ghURL + "/mcp/", "catalog_key": "github", "auth": "oauth",
		"config": map[string]string{"github_app": app["connector_id"].(string)}})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, "needs_sign_in", out["status"])
	id := out["id"].(string)

	code, out, _ = e.owner.do("POST", "/api/v1/mcp/servers/"+id+"/sign-in", nil)
	require.Equal(t, http.StatusOK, code, out)
	link, _ := url.Parse(out["url"].(string))
	assert.Equal(t, e.ghURL+"/login/oauth/authorize", link.Scheme+"://"+link.Host+link.Path, "GitHub's own sign-in page")
	q := link.Query()
	assert.Equal(t, "Iv23liMANIFEST", q.Get("client_id"))
	assert.Equal(t, hubPublic+mcpconn.CallbackPath, q.Get("redirect_uri"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotContains(t, out["url"], "manifest-secret")
	e.gh.mu.Lock()
	e.gh.challenge = q.Get("code_challenge")
	e.gh.mu.Unlock()

	// GitHub sends the browser back to the Hub's MCP OAuth callback.
	code, _, h = e.owner.do("GET", "/api/v1/mcp/oauth/callback?"+url.Values{"state": {q.Get("state")}, "code": {"gh-code"}}.Encode(), nil)
	require.Equal(t, http.StatusFound, code)
	assert.Equal(t, "/connectors?mcp="+id, h.Get("Location"))
	code, out, _ = e.owner.do("GET", "/api/v1/mcp/servers", nil)
	require.Equal(t, http.StatusOK, code)
	srv := out["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "ok", srv["status"], srv["last_error"])
	assert.Equal(t, true, srv["signed_in"])
	e.gh.mu.Lock()
	assert.Equal(t, "manifest-secret", e.gh.forms[0].Get("client_secret"))
	assert.False(t, e.gh.forms[0].Has("resource"))
	e.gh.mu.Unlock()

	code, out, _ = e.owner.do("GET", "/api/v1/audit?action=github.connect.app", nil)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, out["items"].([]any), 1)
	code, out, _ = e.owner.do("GET", "/api/v1/audit?action=mcp_server.sign_in", nil)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, out["items"].([]any), 1)

	// Only signed-in admins reach these routes.
	code, _, _ = newClient(t, e.srv.URL).do("GET", "/api/v1/mcp/servers", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
}

// An App connected before the Hub kept its OAuth client: the admin adds the client ID and secret.
func TestGitHubMCP_AddTheClientToAnExistingApp(t *testing.T) {
	e := newGitHubMCPEnv(t)
	ctx := context.Background()
	appID, err := e.conns.Create(ctx, store.NewConnector{Type: "github", Name: "GitHub (acme)", Mode: "poll", Credentials: "PEM",
		Config: map[string]string{"app_id": "7", "app_slug": "docs-acme", "owner": "acme"}})
	require.NoError(t, err)
	pat, err := e.conns.Create(ctx, store.NewConnector{Type: "github", Name: "token", Credentials: "ghp_x"})
	require.NoError(t, err)

	apps := githubApps(t, e.owner)
	require.Len(t, apps, 1)
	assert.Equal(t, false, apps[0].(map[string]any)["oauth"], "listed, but it cannot sign in yet")

	put := func(id string, body map[string]any) (int, map[string]any) {
		code, out, _ := e.owner.do("PUT", "/api/v1/github/connect/"+id+"/oauth-client", body)
		return code, out
	}
	code, out := put(appID, map[string]any{"client_id": "not valid!", "client_secret": "s"})
	assert.Equal(t, http.StatusBadRequest, code, out)
	code, out = put(appID, map[string]any{"client_secret": "s"})
	assert.Equal(t, http.StatusBadRequest, code, out)
	code, out = put(pat, map[string]any{"client_id": "Iv23liEXISTING", "client_secret": "s"})
	assert.Equal(t, http.StatusBadRequest, code, out, "only GitHub Apps")
	code, out = put(appID, map[string]any{"client_id": "Iv23liEXISTING", "client_secret": e.seal(t, "s3cret", secrets.PurposeMCPSecret)})
	assert.Equal(t, http.StatusBadRequest, code, "a value sealed for another purpose does not open")
	assert.Equal(t, "SEAL_INVALID", errCode(out))

	code, out = put(appID, map[string]any{"client_id": "Iv23liEXISTING", "client_secret": e.seal(t, "s3cret", secrets.PurposeConnectorOAuthClient)})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, true, out["oauth"])
	assert.NotContains(t, out, "client_secret")
	c, err := e.conns.GitHubAppClient(ctx, appID)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", c.ClientSecret)
	assert.Equal(t, true, githubApps(t, e.owner)[0].(map[string]any)["oauth"])

	code, out, _ = e.owner.do("GET", "/api/v1/audit?action=github.connect.oauth_client", nil)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, out["items"].([]any), 1)
	assert.NotContains(t, strings.ToLower(jsonString(out)), "s3cret", "the audit log never holds the secret")

	code, _ = put(appID, map[string]any{"client_id": "", "client_secret": ""})
	assert.Equal(t, http.StatusNoContent, code)
	assert.Equal(t, false, githubApps(t, e.owner)[0].(map[string]any)["oauth"])
}

// A GitHub that answers without client credentials still connects the App; it just cannot sign in.
func TestGitHubMCP_ManifestWithoutClient(t *testing.T) {
	e := newGitHubMCPEnv(t)
	e.gh.noClient = true
	code, out, _ := e.owner.do("POST", "/api/v1/github/connect", map[string]any{"base_url": e.ghURL})
	require.Equal(t, http.StatusOK, code, out)
	action, _ := url.Parse(out["action"].(string))
	code, _, _ = e.owner.do("GET", "/api/v1/github/connect/callback?"+url.Values{"state": {action.Query().Get("state")}, "code": {"mcode"}}.Encode(), nil)
	require.Equal(t, http.StatusFound, code)
	apps := githubApps(t, e.owner)
	require.Len(t, apps, 1)
	assert.Equal(t, false, apps[0].(map[string]any)["oauth"])
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

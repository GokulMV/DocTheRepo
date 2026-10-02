package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
)

// TestGitHubOneClickConnect walks the GitHub App manifest flow against the GitHub mock: start → (GitHub
// creates the app) → callback stores it as a connector → (user installs it) → setup records the
// installation → the connector authenticates as the app and lists its repositories.
func TestGitHubOneClickConnect(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	cfg.Server.PublicURL = "https://hub.acme.example"
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), queue.New(st, queue.Options{}), log, m)
	require.NoError(t, err)
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	gh := githubmock.New()
	defer gh.Close()
	gh.CreateRepo("acme/shop", "main", map[string]string{"README.md": "# Shop\n"})
	gh.ManifestCode = "good-code"

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes()}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	code, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)

	// Start: a manifest to post to GitHub (here: the mock acting as GitHub Enterprise Server).
	code, out = c.call("POST", "/github/connect", map[string]any{"org": "acme", "base_url": gh.URL})
	require.Equal(t, http.StatusOK, code, out)
	action, err := url.Parse(out["action"].(string))
	require.NoError(t, err)
	assert.Equal(t, gh.URL+"/organizations/acme/settings/apps/new", action.Scheme+"://"+action.Host+action.Path)
	state := action.Query().Get("state")
	require.NotEmpty(t, state)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal([]byte(out["manifest"].(string)), &manifest))
	assert.Equal(t, "https://hub.acme.example/api/v1/github/connect/callback", manifest["redirect_url"])
	hook := manifest["hook_attributes"].(map[string]any)["url"].(string)
	assert.True(t, strings.HasPrefix(hook, "https://hub.acme.example/hooks/github/"))
	assert.LessOrEqual(t, len(manifest["name"].(string)), 34)
	connID := strings.TrimPrefix(hook, "https://hub.acme.example/hooks/github/")

	// The browser entry: a page that posts the manifest, allowed to post only to that GitHub host.
	resp0, err := c.c.Get(srv.URL + "/api/v1/github/connect/start?org=acme&base_url=" + url.QueryEscape(gh.URL))
	require.NoError(t, err)
	page, _ := io.ReadAll(resp0.Body)
	resp0.Body.Close()
	require.Equal(t, http.StatusOK, resp0.StatusCode)
	csp := resp0.Header.Get("Content-Security-Policy")
	assert.Contains(t, csp, "form-action "+gh.URL)
	assert.Contains(t, csp, "script-src 'nonce-")
	assert.Contains(t, string(page), `name="manifest"`)
	assert.Contains(t, string(page), "/organizations/acme/settings/apps/new?state=")

	get := func(path string) *http.Response {
		t.Helper()
		resp, err := c.c.Get(srv.URL + "/api/v1" + path)
		require.NoError(t, err)
		resp.Body.Close()
		return resp
	}
	// A forged or missing state never creates anything.
	resp := get("/github/connect/callback?code=good-code&state=forged")
	assert.Contains(t, resp.Header.Get("Location"), "github_error=")

	// Callback: GitHub created the app; the Hub stores it and sends the user to install it.
	resp = get("/github/connect/callback?code=good-code&state=" + url.QueryEscape(state))
	require.Equal(t, http.StatusFound, resp.StatusCode)
	install, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, gh.URL+"/github-apps/docTheRepo-test/installations/new", install.Scheme+"://"+install.Host+install.Path)

	cc, err := a.conns.Get(ctx, connID)
	require.NoError(t, err)
	assert.Equal(t, "github", cc.Type)
	assert.Equal(t, "webhook", cc.Mode)
	assert.Equal(t, "777", cc.Config["app_id"])
	assert.Equal(t, gh.URL+"/api/v3/", cc.Config["base_url"])
	assert.Equal(t, "whsec-from-manifest", cc.WebhookSecret)
	assert.Contains(t, cc.Credentials, "PRIVATE KEY")

	// The code is single use.
	resp = get("/github/connect/callback?code=good-code&state=" + url.QueryEscape(state))
	assert.Contains(t, resp.Header.Get("Location"), "github_error=")

	// Setup: installed; the connector works as the app.
	resp = get("/github/connect/setup?installation_id=4242&setup_action=install&state=" + url.QueryEscape(install.Query().Get("state")))
	require.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/connectors?connector="+connID+"&github=connected", resp.Header.Get("Location"))
	code, out = c.call("POST", "/connectors/"+connID+"/test", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, out["ok"], out)
	code, out = c.call("GET", "/connectors/"+connID+"/available-repos", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, []any{map[string]any{"full_name": "acme/shop", "tracked": false}}, out["items"])

	// Disable suspends the installation on GitHub; Enable resumes it.
	code, out = c.call("PATCH", "/connectors/"+connID, map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, map[string]any{"action": "suspended", "ok": true}, out["github"])
	assert.Equal(t, "suspended", gh.Installations["4242"])
	code, out = c.call("PATCH", "/connectors/"+connID, map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, map[string]any{"action": "resumed", "ok": true}, out["github"])
	assert.NotContains(t, gh.Installations, "4242")

	// Remove uninstalls it (also when the connector was disabled first) and points at the App to delete.
	code, _ = c.call("PATCH", "/connectors/"+connID, map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, code)
	code, out = c.call("DELETE", "/connectors/"+connID, nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, map[string]any{"action": "uninstalled", "ok": true,
		"settings_url": gh.URL + "/organizations/acme/settings/apps/dth-hub"}, out["github"])
	assert.Equal(t, "deleted", gh.Installations["4242"])
	_, err = a.conns.GetAny(ctx, connID)
	assert.Error(t, err)
}

func TestGitHubConnectOnLocalhostPolls(t *testing.T) {
	assert.False(t, api.Reachable("http://localhost:8080"))
	assert.False(t, api.Reachable("http://192.168.1.4:8080"))
	assert.True(t, api.Reachable("https://hub.acme.example"))
}

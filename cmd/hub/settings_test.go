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
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
)

// TestSettingsFileEndToEnd applies a settings document through /settings/apply as a signed-in owner: the
// Hub resolves only the references its policy allows, previews, applies through its own API (session and
// CSRF carried over), is idempotent, and exports the result with references instead of secrets.
func TestSettingsFileEndToEnd(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), queue.New(st, queue.Options{}), log, m)
	require.NoError(t, err)
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	gh := githubmock.New()
	defer gh.Close()
	gh.CreateRepo("acme/shop", "main", map[string]string{"README.md": "# Shop\n"})
	t.Setenv("DTH_SECRET_GITHUB", "ghp_from_env")

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes(),
		Settings: &api.SettingsPolicy{Sources: []string{"env"}}}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	code, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)

	doc := `
providers:
  - { name: local, kind: openai_compat, base_url: "http://127.0.0.1:1/v1", api_key: plain-key }
routes:
  qa: { provider: local, model: m1 }
connectors:
  - name: GitHub
    type: github
    mode: poll
    credentials: ${env:DTH_SECRET_GITHUB}
    config: { base_url: "` + gh.APIURL() + `", bot_login: "` + gh.BotLogin + `" }
repos:
  - { full_name: acme/shop, connector: GitHub, docs_path: docs/generated/ }
spend:
  limits:
    - { scope: provider, key: local, window: day, max_tokens: 1000 }
`
	// References the policy does not allow are refused before anything happens.
	code, out = c.call("POST", "/settings/apply", map[string]any{"document": "providers:\n  - { name: x, kind: openai, api_key: \"${env:DTH_DATABASE_URL}\" }\n", "dry_run": true})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Contains(t, out["error"].(map[string]any)["message"], "only variables starting with DTH_SECRET_")
	code, out = c.call("POST", "/settings/apply", map[string]any{"document": "providers:\n  - { name: x, kind: openai, api_key: \"${vault:secret/x#k}\" }\n", "dry_run": true})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Contains(t, out["error"].(map[string]any)["message"], "not enabled on this Hub")

	code, out = c.call("POST", "/settings/apply", map[string]any{"document": doc, "dry_run": true})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, true, out["dry_run"])
	assert.Len(t, out["changes"], 5)
	_, list := c.call("GET", "/providers", nil)
	assert.Empty(t, list["items"], "a preview changes nothing")

	code, out = c.call("POST", "/settings/apply", map[string]any{"document": doc})
	require.Equal(t, http.StatusOK, code, out)
	require.Empty(t, out["error"], out)
	assert.Equal(t, []any{"env"}, out["sources"])
	_, list = c.call("GET", "/repos", nil)
	require.Len(t, list["items"], 1)
	assert.Equal(t, "docs/generated/", list["items"].([]any)[0].(map[string]any)["docs_path"])
	_, list = c.call("GET", "/connectors", nil)
	assert.Equal(t, true, list["items"].([]any)[0].(map[string]any)["has_credentials"])
	_, list = c.call("GET", "/audit?action=repo.create", nil)
	require.Len(t, list["items"], 1)
	entry := list["items"].([]any)[0].(map[string]any)
	assert.NotEmpty(t, entry["actor_user_id"], "inner calls are audited as the caller")
	assert.Equal(t, "127.0.0.1", entry["ip"], "with the caller's address")

	code, out = c.call("POST", "/settings/apply", map[string]any{"document": doc, "dry_run": true})
	require.Equal(t, http.StatusOK, code)
	for _, ch := range out["changes"].([]any) {
		ch := ch.(map[string]any)
		if ch["kind"] == "route" || ch["kind"] == "repo" || ch["kind"] == "spend" {
			assert.Equal(t, "unchanged", ch["action"], ch)
		}
	}

	code, out = c.call("GET", "/settings/export", nil)
	require.Equal(t, http.StatusOK, code, out)
	y := out["yaml"].(string)
	assert.Contains(t, y, "${env:GITHUB_CREDENTIALS}")
	assert.Contains(t, y, "${env:LOCAL_API_KEY}")
	assert.NotContains(t, y, "ghp_from_env")
	assert.NotContains(t, y, "plain-key")
	assert.Contains(t, y, "key: local", "the provider limit is exported by name")
}

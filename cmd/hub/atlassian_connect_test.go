package main

import (
	"context"
	"encoding/json"
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

	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/atlassian"
	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// TestConnectWithAtlassian walks "Connect with Atlassian" through the API against a fake Atlassian: the
// one-time app setup, start → consent → callback creating a Confluence connector, its edits, and who may
// do it.
func TestConnectWithAtlassian(t *testing.T) {
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

	// A fake auth.atlassian.com + api.atlassian.com.
	var tokenBodies []map[string]string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth/token":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			tokenBodies = append(tokenBodies, in)
			if in["client_secret"] != "s3cret-from-console" || in["code"] != "one-time-code" {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600}`))
		case "/oauth/token/accessible-resources":
			_, _ = w.Write([]byte(`[{"id":"11223344-a1b2-3b33-c444-def123456789","url":"https://acme.atlassian.net","name":"acme","scopes":["read:confluence-content.all","search:confluence"]}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	a.atlassian.Endpoints = atlassian.Endpoints{Authorize: fake.URL + "/authorize", Token: fake.URL + "/oauth/token", API: fake.URL}

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes()}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	code, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)

	// Before the one-time setup: the page says what to register; connecting explains what is missing.
	code, out = c.call("GET", "/atlassian/oauth-app", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["configured"])
	assert.Equal(t, "https://hub.acme.example/api/v1/atlassian/connect/callback", out["callback_url"])
	assert.Equal(t, atlassian.ConsoleURL, out["console_url"])
	assert.Contains(t, out["scopes"].(map[string]any)["jira"], "read:jira-work")
	code, out = c.call("POST", "/atlassian/connect", map[string]string{"type": "confluence"})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "ATLASSIAN_APP_MISSING", out["error"].(map[string]any)["code"], out)

	// The setup: the secret is stored sealed and never returned.
	code, out = c.call("PUT", "/atlassian/oauth-app", map[string]string{"client_id": "AbCdEf123456"})
	assert.Equal(t, http.StatusBadRequest, code, "the first save needs the secret")
	code, out = c.call("PUT", "/atlassian/oauth-app", map[string]string{"client_id": "AbCdEf123456", "client_secret": "s3cret-from-console"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, true, out["configured"])
	assert.Equal(t, "AbCdEf123456", out["client_id"])
	raw, _ := json.Marshal(out)
	assert.NotContains(t, string(raw), "s3cret")
	var ct []byte
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT client_secret_ct FROM atlassian_oauth_app`).Scan(&ct))
	assert.NotContains(t, string(ct), "s3cret")
	code, _ = c.call("PUT", "/atlassian/oauth-app", map[string]string{"client_id": "AbCdEf123456"})
	assert.Equal(t, http.StatusOK, code, "later saves may keep the secret")

	// Start: Atlassian's consent page with the state.
	code, out = c.call("POST", "/atlassian/connect", map[string]string{"type": "confluence", "keys": "ENG"})
	require.Equal(t, http.StatusOK, code, out)
	consent, err := url.Parse(out["url"].(string))
	require.NoError(t, err)
	assert.Equal(t, fake.URL+"/authorize", consent.Scheme+"://"+consent.Host+consent.Path)
	assert.Equal(t, "api.atlassian.com", consent.Query().Get("audience"))
	assert.Equal(t, "https://hub.acme.example/api/v1/atlassian/connect/callback", consent.Query().Get("redirect_uri"))
	state := consent.Query().Get("state")

	get := func(path string) string {
		t.Helper()
		resp, err := c.c.Get(srv.URL + "/api/v1" + path)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusFound, resp.StatusCode)
		return resp.Header.Get("Location")
	}
	// Forged state, a refusal on Atlassian's page: nothing is created and Atlassian is not asked.
	assert.Contains(t, get("/atlassian/connect/callback?code=one-time-code&state=forged"), "atlassian_error=")
	assert.Contains(t, get("/atlassian/connect/callback?error=access_denied&state="+url.QueryEscape(state)), "atlassian_error=access+was+not+granted")
	assert.Empty(t, tokenBodies)

	loc := get("/atlassian/connect/callback?code=one-time-code&state=" + url.QueryEscape(state))
	back, err := url.Parse(loc)
	require.NoError(t, err)
	assert.Equal(t, "/connectors", back.Path)
	require.Equal(t, "connected", back.Query().Get("atlassian"), loc)
	id := back.Query().Get("connector")
	cc, err := a.conns.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "confluence", cc.Type)
	assert.Equal(t, "oauth", cc.Config["auth"])
	assert.Equal(t, "https://acme.atlassian.net/wiki", cc.Config["base_url"])
	assert.Equal(t, "11223344-a1b2-3b33-c444-def123456789", cc.Config["cloud_id"])
	assert.Contains(t, cc.Credentials, `"refresh_token":"rt-1"`)
	assert.Equal(t, "https://hub.acme.example/api/v1/atlassian/connect/callback", tokenBodies[0]["redirect_uri"])

	// The list shows the connector without its tokens; the state does not work twice.
	code, out = c.call("GET", "/connectors", nil)
	require.Equal(t, http.StatusOK, code)
	raw, _ = json.Marshal(out)
	assert.NotContains(t, string(raw), "rt-1")
	assert.NotContains(t, string(raw), "at-1")
	assert.Contains(t, get("/atlassian/connect/callback?code=one-time-code&state="+url.QueryEscape(state)), "atlassian_error=")

	// Editing the spaces keeps the site and sign-in; tokens cannot be overwritten by hand.
	code, out = c.call("PATCH", "/connectors/"+id, map[string]any{"config": map[string]string{"spaces": "ENG, OPS", "cloud_id": "evil", "auth": ""}})
	require.Equal(t, http.StatusOK, code, out)
	cc, _ = a.conns.Get(ctx, id)
	assert.Equal(t, "ENG, OPS", cc.Config["spaces"])
	assert.Equal(t, "11223344-a1b2-3b33-c444-def123456789", cc.Config["cloud_id"])
	assert.Equal(t, "oauth", cc.Config["auth"])
	code, _ = c.call("PATCH", "/connectors/"+id, map[string]any{"credentials": "tok"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = c.call("POST", "/connectors", map[string]any{"type": "jira", "name": "J", "config": map[string]string{"auth": "oauth"}, "credentials": "{}"})
	assert.Equal(t, http.StatusBadRequest, code, "OAuth connectors come only from the sign-in")

	// Only admins: a viewer may neither read the setup nor connect.
	viewer, err := a.auth.UpsertOIDCUser(ctx, auth.Claims{Subject: "v1", Email: "viewer@acme.com", Name: "Vee"})
	require.NoError(t, err)
	tok, _, err := a.auth.CreateToken(ctx, viewer.ID, "t", 0)
	require.NoError(t, err)
	for _, r := range [][2]string{{"GET", "/atlassian/oauth-app"}, {"POST", "/atlassian/connect"}, {"GET", "/atlassian/connect/callback"}} {
		req, _ := http.NewRequest(r[0], srv.URL+"/api/v1"+r[1], strings.NewReader(`{"type":"jira"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, r[1])
	}

	// Removing the app.
	code, _ = c.call("DELETE", "/atlassian/oauth-app", nil)
	assert.Equal(t, http.StatusNoContent, code)
	_, out = c.call("GET", "/atlassian/oauth-app", nil)
	assert.Equal(t, false, out["configured"])
}

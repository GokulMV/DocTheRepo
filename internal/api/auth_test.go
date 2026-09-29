package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/oidcmock"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
	pat  string
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *client) do(method, path string, body any) (int, map[string]any, http.Header) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		u = c.base + path
	}
	req, _ := http.NewRequest(method, u, rd)
	if c.csrf != "" {
		req.Header.Set(api.CSRFHeader, c.csrf)
	}
	if c.pat != "" {
		req.Header.Set("Authorization", "Bearer "+c.pat)
	}
	resp, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp.Header
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

type authEnv struct {
	st  *store.Store
	svc *auth.Service
	srv *httptest.Server
	idp *oidcmock.Server
	cfg config.AuthConfig
}

func newAuthEnv(t *testing.T, mode string, allRead bool) *authEnv {
	t.Helper()
	st := storetest.New(t)
	cfg := config.Default().Auth
	cfg.Mode, cfg.AllUsersReadAllRepos = mode, allRead
	e := &authEnv{st: st, cfg: cfg}
	d := api.Deps{Log: slog.New(slog.DiscardHandler), Metrics: observability.NewMetrics()}
	var srv *httptest.Server
	if mode == "oidc" {
		e.idp = oidcmock.New("hub")
		t.Cleanup(e.idp.Close)
		srv = httptest.NewUnstartedServer(nil)
		cfg.OIDC.Issuer, cfg.OIDC.ClientID = e.idp.URL, "hub"
		cfg.OIDC.RedirectURL = "http://" + srv.Listener.Addr().String() + "/api/v1/auth/callback"
		cfg.OIDC.AllowedDomains = []string{"acme.com"}
		o, err := auth.NewOIDC(context.Background(), cfg.OIDC, "secret")
		require.NoError(t, err)
		d.OIDC = o
	} else {
		srv = httptest.NewUnstartedServer(nil)
	}
	e.svc = auth.New(st, cfg)
	d.Auth = e.svc
	srv.Config.Handler = api.NewRouter(d)
	srv.Start()
	t.Cleanup(srv.Close)
	e.srv = srv
	return e
}

func (e *authEnv) localLogin(t *testing.T, email, pw string) *client {
	c := newClient(t, e.srv.URL)
	code, out, _ := c.do("POST", "/api/v1/auth/local/login", map[string]string{"email": email, "password": pw})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)
	return c
}

// ssoLogin runs the browser flow: Hub → IdP authorize → Hub callback.
func (e *authEnv) ssoLogin(t *testing.T, u oidcmock.User) (*client, int, map[string]any) {
	e.idp.User = u
	c := newClient(t, e.srv.URL)
	code, _, h := c.do("GET", "/api/v1/auth/login?return=/docs", nil)
	require.Equal(t, http.StatusFound, code)
	loc := h.Get("Location")
	assert.Contains(t, loc, "code_challenge_method=S256")
	code, _, h = c.do("GET", loc, nil)
	require.Equal(t, http.StatusFound, code)
	code, out, h := c.do("GET", h.Get("Location"), nil)
	if code == http.StatusFound {
		assert.Equal(t, "/docs", h.Get("Location"))
		_, me, _ := c.do("GET", "/api/v1/me", nil)
		if tok, ok := me["csrf_token"].(string); ok {
			c.csrf = tok
		}
	}
	return c, code, out
}

func TestLocalLoginSessionsCSRFAndTokens(t *testing.T) {
	e := newAuthEnv(t, "local", true)
	ctx := context.Background()
	_, err := e.svc.BootstrapOwner(ctx, "owner@acme.com", "short")
	assert.Error(t, err, "weak passwords are refused")
	_, err = e.svc.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)

	anon := newClient(t, e.srv.URL)
	code, out, _ := anon.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "UNAUTHENTICATED", errCode(out))
	code, out, _ = anon.do("POST", "/api/v1/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "wrong password!!"})
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "INVALID_CREDENTIALS", errCode(out))
	code, out, _ = anon.do("POST", "/api/v1/auth/local/login", map[string]string{"email": "nobody@acme.com", "password": "wrong password!!"})
	assert.Equal(t, "INVALID_CREDENTIALS", errCode(out), "unknown users look the same as wrong passwords")
	code, _, _ = anon.do("GET", "/api/v1/auth/login", nil)
	assert.Equal(t, http.StatusNotFound, code, "no SSO configured")

	c := e.localLogin(t, "owner@acme.com", "correct horse battery staple")
	code, me, _ := c.do("GET", "/api/v1/me", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "owner", me["role"])
	assert.Equal(t, true, me["repo_access"].(map[string]any)["all"])

	csrf := c.csrf
	c.csrf = ""
	code, out, _ = c.do("POST", "/api/v1/tokens", map[string]any{"name": "cli"})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "CSRF_FAILED", errCode(out), "cookie-authenticated mutations need the CSRF header")
	c.csrf = csrf
	code, out, _ = c.do("POST", "/api/v1/tokens", map[string]any{"name": "cli", "expires_in_days": 30})
	require.Equal(t, http.StatusCreated, code, out)
	pat := out["token"].(string)
	assert.True(t, strings.HasPrefix(pat, auth.TokenPrefix))
	code, _, _ = c.do("POST", "/api/v1/tokens", map[string]any{"name": ""})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _, _ = c.do("POST", "/api/v1/tokens", map[string]any{"name": "x", "bogus": 1})
	assert.Equal(t, http.StatusBadRequest, code, "unknown fields are rejected")

	bearer := newClient(t, e.srv.URL)
	bearer.pat = pat
	code, me, _ = bearer.do("GET", "/api/v1/me", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Nil(t, me["csrf_token"], "token callers get no CSRF token")
	code, list, _ := bearer.do("GET", "/api/v1/tokens", nil)
	require.Equal(t, http.StatusOK, code)
	items := list["items"].([]any)
	require.Len(t, items, 1)
	assert.Nil(t, items[0].(map[string]any)["token"], "secrets are never listed")
	id := items[0].(map[string]any)["id"].(string)
	code, _, _ = bearer.do("DELETE", "/api/v1/tokens/"+id, nil)
	assert.Equal(t, http.StatusNoContent, code, "token callers need no CSRF")
	code, out, _ = bearer.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code, "revoked")
	bearer.pat = "dth_pat_bogus"
	code, _, _ = bearer.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code)

	code, _, _ = c.do("POST", "/api/v1/auth/logout", nil)
	assert.Equal(t, http.StatusNoContent, code)
	code, _, _ = c.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code)

	audit := e.localLogin(t, "owner@acme.com", "correct horse battery staple")
	code, out, _ = audit.do("GET", "/api/v1/audit?action=token.create", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 1)
	code, out, _ = audit.do("GET", "/api/v1/audit?limit=1", nil)
	require.Equal(t, http.StatusOK, code)
	next, _ := out["next_cursor"].(string)
	require.NotEmpty(t, next)
	code, out, _ = audit.do("GET", "/api/v1/audit?limit=1&cursor="+url.QueryEscape(next), nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 1)
	code, _, _ = audit.do("GET", "/api/v1/audit?cursor=!!", nil)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestSessionExpiry(t *testing.T) {
	e := newAuthEnv(t, "local", true)
	ctx := context.Background()
	_, err := e.svc.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	c := e.localLogin(t, "owner@acme.com", "correct horse battery staple")
	e.svc.Now = func() time.Time { return time.Now().Add(e.cfg.SessionIdle + time.Minute) }
	code, _, _ := c.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code, "idle sessions expire")
	require.NoError(t, e.svc.GCSessions(ctx))
}

func TestOIDCLoginRolesAndACL(t *testing.T) {
	e := newAuthEnv(t, "oidc", false)
	ctx := context.Background()
	anon := newClient(t, e.srv.URL)
	code, out, _ := anon.do("POST", "/api/v1/auth/local/login", map[string]string{"email": "a", "password": "b"})
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "LOCAL_LOGIN_DISABLED", errCode(out))

	owner, code, _ := e.ssoLogin(t, oidcmock.User{Subject: "u1", Email: "ann@acme.com", EmailVerified: true, Name: "Ann"})
	require.Equal(t, http.StatusFound, code)
	_, me, _ := owner.do("GET", "/api/v1/me", nil)
	assert.Equal(t, "owner", me["role"], "the first user becomes the owner")

	viewer, code, _ := e.ssoLogin(t, oidcmock.User{Subject: "u2", Email: "bob@acme.com", EmailVerified: true, Name: "Bob", Groups: []string{"payments"}})
	require.Equal(t, http.StatusFound, code)
	_, me, _ = viewer.do("GET", "/api/v1/me", nil)
	assert.Equal(t, "viewer", me["role"])
	assert.Equal(t, false, me["repo_access"].(map[string]any)["all"])
	code, out, _ = viewer.do("GET", "/api/v1/users", nil)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "FORBIDDEN", errCode(out))

	_, code, out = e.ssoLogin(t, oidcmock.User{Subject: "u3", Email: "eve@evil.com", EmailVerified: true})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "DOMAIN_NOT_ALLOWED", errCode(out))
	_, code, out = e.ssoLogin(t, oidcmock.User{Subject: "u4", Email: "carl@acme.com", EmailVerified: false})
	assert.Equal(t, "EMAIL_NOT_VERIFIED", errCode(out))
	e.idp.WrongNonce = true
	_, code, out = e.ssoLogin(t, oidcmock.User{Subject: "u1", Email: "ann@acme.com", EmailVerified: true})
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "OIDC_FAILED", errCode(out), "a replayed ID token is rejected")
	e.idp.WrongNonce = false
	code, out, _ = anon.do("GET", "/api/v1/auth/callback?state=forged&code=x", nil)
	assert.Equal(t, "LOGIN_STATE_INVALID", errCode(out))

	// Group-based and direct repo grants (all_users_read_all_repos: false).
	connID, repoA := seedRepo(t, e.st, "acme/payments")
	_, repoB := seedRepoOn(t, e.st, connID, "acme/web")
	_, err := e.st.Pool.Exec(ctx, `INSERT INTO group_repo_access (group_id, repo_id) SELECT id, $1 FROM groups WHERE name = 'payments'`, repoA)
	require.NoError(t, err)
	_, me, _ = viewer.do("GET", "/api/v1/me", nil)
	assert.Equal(t, []any{repoA}, me["repo_access"].(map[string]any)["repo_ids"])

	code, users, _ := owner.do("GET", "/api/v1/users", nil)
	require.Equal(t, http.StatusOK, code)
	var bobID, annID string
	for _, u := range users["items"].([]any) {
		m := u.(map[string]any)
		switch m["email"] {
		case "bob@acme.com":
			bobID = m["id"].(string)
		case "ann@acme.com":
			annID = m["id"].(string)
		}
	}
	code, _, _ = owner.do("PUT", "/api/v1/users/"+bobID+"/repo-access", map[string]any{"repo_ids": []string{repoB}})
	require.Equal(t, http.StatusNoContent, code)
	_, me, _ = viewer.do("GET", "/api/v1/me", nil)
	assert.ElementsMatch(t, []any{repoA, repoB}, me["repo_access"].(map[string]any)["repo_ids"])

	code, out, _ = owner.do("PATCH", "/api/v1/users/"+annID, map[string]any{"role": "viewer"})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "LAST_OWNER", errCode(out))
	code, out, _ = owner.do("PATCH", "/api/v1/users/"+bobID, map[string]any{"role": "admin"})
	require.Equal(t, http.StatusOK, code, out)
	code, _, _ = owner.do("PATCH", "/api/v1/users/"+bobID, map[string]any{"role": "wizard"})
	assert.Equal(t, http.StatusBadRequest, code)
	_, me, _ = viewer.do("GET", "/api/v1/me", nil)
	assert.Equal(t, true, me["repo_access"].(map[string]any)["all"], "admins read every repo")
	code, _, _ = viewer.do("PATCH", "/api/v1/users/"+annID, map[string]any{"disabled": true})
	assert.Equal(t, http.StatusForbidden, code, "only owners may change owners")
	code, _, _ = owner.do("PATCH", "/api/v1/users/"+bobID, map[string]any{"disabled": true})
	require.Equal(t, http.StatusOK, code)
	code, _, _ = viewer.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code, "disabling ends sessions")
	code, _, _ = owner.do("PATCH", "/api/v1/users/00000000-0000-0000-0000-000000000000", map[string]any{"disabled": true})
	assert.Equal(t, http.StatusNotFound, code)
}

func TestSafeReturn(t *testing.T) {
	e := newAuthEnv(t, "oidc", true)
	e.idp.User = oidcmock.User{Subject: "u1", Email: "ann@acme.com", EmailVerified: true}
	c := newClient(t, e.srv.URL)
	_, _, h := c.do("GET", "/api/v1/auth/login?return=//evil.example/x", nil)
	_, _, h = c.do("GET", h.Get("Location"), nil)
	_, _, h = c.do("GET", h.Get("Location"), nil)
	assert.Equal(t, "/", h.Get("Location"), "no open redirect")
}

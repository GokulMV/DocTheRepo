package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/oidcmock"
)

// A Hub deployed with a settings file comes up with single sign-on, its owner and a model in place, before
// anyone has signed in or added a repository: the listed owner signs in with SSO and is the owner.
func TestSettingsAppliedAtStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	idp := oidcmock.New("hub")
	defer idp.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "hub.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`
auth:
  password: false
  sso:
    provider: okta
    issuer: `+idp.URL+`
    client_id: hub
    client_secret: ${env:TEST_OIDC_SECRET}
    allowed_domains: [acme.com]
users:
  - email: Ann@acme.com
    name: Ann
    role: owner
  - email: bo@acme.com
    role: editor
providers:
  - name: Claude
    kind: anthropic
    api_key: ${env:TEST_ANTHROPIC_KEY}
`), 0o600))
	t.Setenv("TEST_OIDC_SECRET", "s3cret")
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-test")

	srv := httptest.NewUnstartedServer(nil)
	cfg := config.Default()
	cfg.Auth.Mode = "local" // a fresh quickstart-style Hub: the file turns passwords off and SSO on
	cfg.Server.PublicURL = "http://" + srv.Listener.Addr().String()
	cfg.Settings.ApplyOnStart = []string{dir}
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), queue.New(st, queue.Options{}), log, m)
	require.NoError(t, err)
	h := api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, OIDC: a.oidc, PublicURL: cfg.Server.PublicURL, SealKeys: a.sealKeys, V1: a.v1Routes()})
	srv.Config.Handler = h
	srv.Start()
	defer srv.Close()
	applyStartupSettings(ctx, cfg.Settings, h, log)

	require.Eventually(t, func() bool { return a.auth.OIDC() != nil }, 10*time.Second, 50*time.Millisecond, "single sign-on is configured")
	require.Eventually(t, func() bool { n, _ := st.Q.CountUsers(ctx); return n == 2 }, 10*time.Second, 50*time.Millisecond)
	assert.False(t, a.auth.PasswordEnabled(ctx), "passwords off, as the file says")
	provs, err := st.Q.ListProviders(ctx)
	require.NoError(t, err)
	require.Len(t, provs, 1)

	// The listed owner signs in through SSO and owns the Hub.
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	idp.User = oidcmock.User{Subject: "ann-1", Email: "ann@acme.com", EmailVerified: true, Name: "Ann"}
	loc := srv.URL + "/api/v1/auth/login?return=/ask"
	for i := 0; i < 3; i++ {
		resp, err := c.Get(loc)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusFound, resp.StatusCode, loc)
		loc = resp.Header.Get("Location")
		if loc == "/ask" {
			break
		}
	}
	require.Equal(t, "/ask", loc, "signed in through the identity provider")
	ac := &apiClient{t: t, base: srv.URL + "/api/v1", c: c}
	_, me := ac.call("GET", "/me", nil)
	assert.Equal(t, "owner", me["role"])

	// Applying again (the next start) changes nothing but the write-only secrets.
	before, _ := st.Q.CountUsers(ctx)
	applyStartupSettings(ctx, cfg.Settings, h, log)
	time.Sleep(500 * time.Millisecond)
	after, _ := st.Q.CountUsers(ctx)
	assert.Equal(t, before, after)
}

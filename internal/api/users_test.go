package api_test

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/test/mocks/oidcmock"
)

func userByEmail(t *testing.T, c *client, email string) map[string]any {
	t.Helper()
	code, out, _ := c.do("GET", "/api/v1/users", nil)
	require.Equal(t, http.StatusOK, code)
	for _, it := range out["items"].([]any) {
		if u := it.(map[string]any); u["email"] == email {
			return u
		}
	}
	t.Fatalf("user %s not listed", email)
	return nil
}

func TestUsersInviteResetAndRemove(t *testing.T) {
	e := newAuthEnv(t, "local", true)
	_, err := e.svc.BootstrapOwner(context.Background(), "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	owner := e.localLogin(t, "owner@acme.com", "correct horse battery staple")

	// Add a user with an invite link.
	code, out, _ := owner.do("POST", "/api/v1/users", map[string]any{"email": " Ann@Acme.com ", "name": "Ann", "role": "admin", "invite": true})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, "ann@acme.com", out["user"].(map[string]any)["email"], "emails are normalized")
	path := out["invite"].(map[string]any)["path"].(string)
	require.True(t, strings.HasPrefix(path, "/invite/"))
	token := strings.TrimPrefix(path, "/invite/")
	ann := userByEmail(t, owner, "ann@acme.com")
	assert.Equal(t, true, ann["invite_pending"])
	assert.Equal(t, false, ann["has_password"])

	code, out, _ = owner.do("POST", "/api/v1/users", map[string]any{"email": "ann@acme.com"})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "USER_EXISTS", errCode(out))
	code, out, _ = owner.do("POST", "/api/v1/users", map[string]any{"email": "not an email"})
	assert.Equal(t, http.StatusBadRequest, code, out)

	// The link: check it, refuse a weak password, then set one and get signed in.
	anon := newClient(t, e.srv.URL)
	code, out, _ = anon.do("GET", "/api/v1/auth/invite/"+token, nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "ann@acme.com", out["email"])
	code, _, _ = anon.do("POST", "/api/v1/auth/invite/"+token, map[string]string{"password": "short"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, out, _ = anon.do("POST", "/api/v1/auth/invite/"+token, map[string]string{"password": "anns long passphrase"})
	require.Equal(t, http.StatusOK, code, out)
	code, me, _ := anon.do("GET", "/api/v1/me", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "admin", me["role"])
	code, out, _ = anon.do("GET", "/api/v1/auth/invite/"+token, nil)
	assert.Equal(t, http.StatusGone, code, "a link works once")
	assert.Equal(t, "INVITE_INVALID", errCode(out))
	code, _, _ = anon.do("GET", "/api/v1/auth/invite/nonsense", nil)
	assert.Equal(t, http.StatusGone, code)
	annC := e.localLogin(t, "ann@acme.com", "anns long passphrase")

	// Reset: a new link replaces the password and ends existing sessions.
	annID := ann["id"].(string)
	code, out, _ = owner.do("POST", "/api/v1/users/"+annID+"/invite", nil)
	require.Equal(t, http.StatusCreated, code, out)
	reset := strings.TrimPrefix(out["path"].(string), "/invite/")
	code, _, _ = newClient(t, e.srv.URL).do("POST", "/api/v1/auth/invite/"+reset, map[string]string{"password": "a brand new passphrase"})
	require.Equal(t, http.StatusOK, code)
	code, _, _ = annC.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code, "the reset signed out old sessions")
	annC = e.localLogin(t, "ann@acme.com", "a brand new passphrase")

	// Edit the name.
	code, out, _ = owner.do("PATCH", "/api/v1/users/"+annID, map[string]any{"name": "Ann Lee"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "Ann Lee", out["name"])

	// Owner protections: an admin cannot add, reset or remove an owner, nor remove themselves.
	code, _, _ = annC.do("POST", "/api/v1/users", map[string]any{"email": "o2@acme.com", "role": "owner"})
	assert.Equal(t, http.StatusForbidden, code)
	ownerID := userByEmail(t, owner, "owner@acme.com")["id"].(string)
	code, _, _ = annC.do("POST", "/api/v1/users/"+ownerID+"/invite", nil)
	assert.Equal(t, http.StatusForbidden, code, "an admin cannot take over an owner's account")
	code, _, _ = annC.do("DELETE", "/api/v1/users/"+ownerID, nil)
	assert.Equal(t, http.StatusForbidden, code)
	code, out, _ = annC.do("DELETE", "/api/v1/users/"+annID, nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "CANNOT_REMOVE_SELF", errCode(out))

	// A viewer has no access to user management.
	code, out, _ = owner.do("POST", "/api/v1/users", map[string]any{"email": "vic@acme.com", "invite": true})
	require.Equal(t, http.StatusCreated, code)
	vicToken := strings.TrimPrefix(out["invite"].(map[string]any)["path"].(string), "/invite/")
	vic := newClient(t, e.srv.URL)
	code, out, _ = vic.do("POST", "/api/v1/auth/invite/"+vicToken, map[string]string{"password": "victors passphrase"})
	require.Equal(t, http.StatusOK, code)
	vic.csrf = out["csrf_token"].(string)
	code, _, _ = vic.do("POST", "/api/v1/users", map[string]any{"email": "x@acme.com"})
	assert.Equal(t, http.StatusForbidden, code)

	// Remove: their sessions end and they cannot sign in.
	code, _, _ = owner.do("DELETE", "/api/v1/users/"+annID, nil)
	require.Equal(t, http.StatusNoContent, code)
	code, _, _ = annC.do("GET", "/api/v1/me", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	code, _, _ = newClient(t, e.srv.URL).do("POST", "/api/v1/auth/local/login", map[string]string{"email": "ann@acme.com", "password": "a brand new passphrase"})
	assert.Equal(t, http.StatusUnauthorized, code)
	code, out, _ = owner.do("DELETE", "/api/v1/users/"+ownerID, nil)
	assert.Equal(t, http.StatusBadRequest, code, "nobody removes themselves (the last owner included)")
	code, out, _ = owner.do("GET", "/api/v1/audit?action=user.delete", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"].([]any), 1)
}

// In OIDC mode people sign in with SSO: an added user is matched by email on their first login, with
// the role the admin gave them; there are no password links.
func TestUsersAddedBeforeSSOLogin(t *testing.T) {
	e := newAuthEnv(t, "oidc", true)
	owner, code, _ := e.ssoLogin(t, oidcmock.User{Subject: "o1", Email: "owner@acme.com", EmailVerified: true, Name: "Owner"})
	require.Equal(t, http.StatusFound, code)
	code, out, _ := owner.do("POST", "/api/v1/users", map[string]any{"email": "bo@acme.com", "role": "editor", "invite": true})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Nil(t, out["invite"], "no password link when password sign-in is off")
	id := out["user"].(map[string]any)["id"].(string)
	code, out, _ = owner.do("POST", "/api/v1/users/"+id+"/invite", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "PASSWORD_LOGIN_DISABLED", errCode(out))

	bo, code, _ := e.ssoLogin(t, oidcmock.User{Subject: "b1", Email: "bo@acme.com", EmailVerified: true, Name: "Bo"})
	require.Equal(t, http.StatusFound, code)
	_, me, _ := bo.do("GET", "/api/v1/me", nil)
	assert.Equal(t, "editor", me["role"], "the pre-assigned role applies")
	assert.Equal(t, id, me["id"])
}

// An owner sets up single sign-on in the UI on a Hub that started with passwords only, then turns
// passwords off; changes that would lock everyone out are refused.
func TestSignInSettings(t *testing.T) {
	e := newAuthEnv(t, "local", true)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	e.svc.Box = secrets.NewBox(kek)
	_, err = e.svc.BootstrapOwner(context.Background(), "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	owner := e.localLogin(t, "owner@acme.com", "correct horse battery staple")

	code, out, _ := owner.do("GET", "/api/v1/auth/settings", nil)
	require.Equal(t, http.StatusOK, code, out)
	st := out["state"].(map[string]any)
	assert.Equal(t, true, st["password"])
	assert.Equal(t, false, st["sso"])
	assert.Equal(t, e.srv.URL+"/api/v1/auth/callback", out["callback_url"], "the URL to register with the identity provider")

	code, out, _ = owner.do("PUT", "/api/v1/auth/settings", map[string]any{"password": false})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "LOCKOUT", errCode(out))

	idp := oidcmock.New("hub")
	t.Cleanup(idp.Close)
	code, out, _ = owner.do("PUT", "/api/v1/auth/settings", map[string]any{"sso": map[string]any{"provider": "okta", "issuer": "https://nowhere.invalid", "client_id": "hub", "client_secret": "s"}})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "OIDC_DISCOVERY_FAILED", errCode(out), "a wrong issuer never replaces the setup")
	code, out, _ = owner.do("PUT", "/api/v1/auth/settings", map[string]any{"sso": map[string]any{"provider": "keycloak", "issuer": idp.URL, "client_id": "hub", "client_secret": "s3cret",
		"allowed_domains": []string{" @Acme.com "}}})
	require.Equal(t, http.StatusOK, code, out)
	st = out["state"].(map[string]any)
	assert.Equal(t, true, st["sso"])
	assert.Equal(t, "settings", st["sso_source"])
	set := st["settings"].(map[string]any)
	assert.Equal(t, true, set["has_secret"])
	assert.Nil(t, set["client_secret"], "the secret is never returned")
	assert.Equal(t, []any{"acme.com"}, set["allowed_domains"])
	_, cfg, _ := newClient(t, e.srv.URL).do("GET", "/api/v1/auth/config", nil)
	assert.Equal(t, true, cfg["sso"])

	// SSO works right away, with the domain allow-list saved here.
	e.idp = idp
	bo, code, _ := e.ssoLogin(t, oidcmock.User{Subject: "b1", Email: "bo@acme.com", EmailVerified: true, Name: "Bo"})
	require.Equal(t, http.StatusFound, code)
	_, me, _ := bo.do("GET", "/api/v1/me", nil)
	assert.Equal(t, "viewer", me["role"])
	_, code, out = e.ssoLogin(t, oidcmock.User{Subject: "x1", Email: "x@other.com", EmailVerified: true})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "DOMAIN_NOT_ALLOWED", errCode(out))

	// Saving other fields keeps the stored secret.
	code, out, _ = owner.do("PUT", "/api/v1/auth/settings", map[string]any{"sso": map[string]any{"provider": "keycloak", "issuer": idp.URL, "client_id": "hub", "allowed_domains": []string{"acme.com"}}})
	require.Equal(t, http.StatusOK, code, out)

	// Passwords off: password login stops; SSO still works.
	code, out, _ = owner.do("PUT", "/api/v1/auth/settings", map[string]any{"password": false})
	require.Equal(t, http.StatusOK, code, out)
	code, out, _ = newClient(t, e.srv.URL).do("POST", "/api/v1/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "LOCAL_LOGIN_DISABLED", errCode(out))
	code, out, _ = owner.do("PUT", "/api/v1/auth/settings", map[string]any{"clear_sso": true})
	assert.Equal(t, "LOCKOUT", errCode(out), "removing SSO while passwords are off is refused")

	// Only owners manage sign-in.
	code, _, _ = owner.do("PATCH", "/api/v1/users/"+me["id"].(string), map[string]any{"role": "admin"})
	require.Equal(t, http.StatusOK, code)
	bo, _, _ = e.ssoLogin(t, oidcmock.User{Subject: "b1", Email: "bo@acme.com", EmailVerified: true, Name: "Bo"})
	code, _, _ = bo.do("GET", "/api/v1/auth/settings", nil)
	assert.Equal(t, http.StatusForbidden, code)

	// After a restart the saved settings apply again.
	fresh := auth.New(e.st, e.cfg)
	fresh.Box = e.svc.Box
	require.NoError(t, fresh.LoadSignIn(context.Background(), nil))
	assert.NotNil(t, fresh.OIDC())
	assert.False(t, fresh.PasswordEnabled(context.Background()))
}

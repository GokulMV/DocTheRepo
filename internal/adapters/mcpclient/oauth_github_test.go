package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeGitHub is GitHub's web application flow for a GitHub App as documented: the token endpoint takes
// client_id + client_secret + code (+ code_verifier) as form fields, answers JSON when asked, issues ghu_
// tokens that expire in 8 hours with single-use ghr_ refresh tokens, and reports errors with HTTP 200 and
// an "error" field (bad_verification_code, bad_refresh_token).
type fakeGitHub struct {
	mu        sync.Mutex
	challenge string // from the authorize link the test followed
	codes     map[string]bool
	refresh   map[string]bool // live refresh tokens
	n         int
	forms     []url.Values
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{codes: map[string]bool{"the-code": true}, refresh: map[string]bool{}}
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.URL.Path != "/login/oauth/access_token" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	g.forms = append(g.forms, r.PostForm)
	w.Header().Set("Content-Type", "application/json")
	answer := func(v map[string]any) { _ = json.NewEncoder(w).Encode(v) }
	if r.PostForm.Get("client_id") != "Iv23liAPPCLIENT" || r.PostForm.Get("client_secret") != "app-secret" {
		answer(map[string]any{"error": "incorrect_client_credentials", "error_description": "The client_id and/or client_secret passed are incorrect."})
		return
	}
	issue := func() {
		g.n++
		rt := "ghr_" + string(rune('0'+g.n))
		g.refresh[rt] = true
		answer(map[string]any{"access_token": "ghu_" + string(rune('0'+g.n)), "expires_in": 28800, "refresh_token": rt,
			"refresh_token_expires_in": 15897600, "scope": "", "token_type": "bearer"})
	}
	switch r.PostForm.Get("grant_type") {
	case "": // the code exchange: GitHub needs no grant_type, accepts authorization_code
		fallthrough
	case "authorization_code":
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !g.codes[r.PostForm.Get("code")] || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			answer(map[string]any{"error": "bad_verification_code", "error_description": "The code passed is incorrect or expired."})
			return
		}
		delete(g.codes, r.PostForm.Get("code"))
		issue()
	case "refresh_token":
		rt := r.PostForm.Get("refresh_token")
		if !g.refresh[rt] {
			answer(map[string]any{"error": "bad_refresh_token", "error_description": "The refresh token passed is incorrect or expired."})
			return
		}
		delete(g.refresh, rt) // single use: rotated
		issue()
	default:
		answer(map[string]any{"error": "unsupported_grant_type"})
	}
}

func TestOAuth_GitHubAppWebFlow(t *testing.T) {
	g := newFakeGitHub()
	gh := httptest.NewServer(g)
	defer gh.Close()
	ctx := context.Background()
	const redirect = "https://hub.example/api/v1/mcp/oauth/callback"

	o := GitHubApp(gh.URL+"/", "Iv23liAPPCLIENT", "app-secret")
	require.Equal(t, gh.URL+"/login/oauth/authorize", o.AuthEndpoint)
	require.Equal(t, gh.URL+"/login/oauth/access_token", o.TokenEndpoint)
	link, err := o.Begin("srv.state", redirect)
	require.NoError(t, err)
	u, _ := url.Parse(link)
	q := u.Query()
	require.Equal(t, gh.URL+"/login/oauth/authorize", u.Scheme+"://"+u.Host+u.Path)
	require.Equal(t, "Iv23liAPPCLIENT", q.Get("client_id"))
	require.Equal(t, redirect, q.Get("redirect_uri"), "must match a callback URL registered on the App")
	require.Equal(t, "srv.state", q.Get("state"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.False(t, q.Has("resource"), "GitHub Apps take no resource indicator")
	require.False(t, q.Has("scope"), "GitHub Apps take no scopes")
	require.NotContains(t, link, "app-secret")
	g.challenge = q.Get("code_challenge")

	require.NoError(t, o.Finish(ctx, nil, "the-code"))
	require.Equal(t, "ghu_1", o.AccessToken)
	require.Equal(t, "ghr_1", o.RefreshToken)
	require.InDelta(t, 28800, time.Until(o.Expiry).Seconds(), 5, "8-hour user token")
	f := g.forms[0]
	require.Equal(t, "app-secret", f.Get("client_secret"), "client_secret_post")
	require.Equal(t, redirect, f.Get("redirect_uri"))
	require.False(t, f.Has("resource"))

	// Refresh rotates the refresh token; the old one stops working.
	require.NoError(t, o.Refresh(ctx, nil))
	require.Equal(t, "ghu_2", o.AccessToken)
	require.Equal(t, "ghr_2", o.RefreshToken, "the new refresh token replaces the spent one")
	require.Equal(t, "refresh_token", g.forms[1].Get("grant_type"))
	require.Equal(t, "app-secret", g.forms[1].Get("client_secret"))

	spent := *o
	spent.RefreshToken = "ghr_1"
	require.ErrorIs(t, spent.Refresh(ctx, nil), ErrSignInAgain, "bad_refresh_token (HTTP 200) means sign in again")

	// A wrong secret is an error to fix, not a lapsed sign-in.
	wrong := *o
	wrong.ClientSecret = "old"
	err = wrong.Refresh(ctx, nil)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrSignInAgain)
	require.Contains(t, err.Error(), "incorrect_client_credentials")
}

func TestOAuth_GitHubCodeReplayFails(t *testing.T) {
	g := newFakeGitHub()
	gh := httptest.NewServer(g)
	defer gh.Close()
	o := GitHubApp(gh.URL, "Iv23liAPPCLIENT", "app-secret")
	link, err := o.Begin("s.t", "https://hub.example/cb")
	require.NoError(t, err)
	u, _ := url.Parse(link)
	g.challenge = "not-" + u.Query().Get("code_challenge") // a code issued for another verifier
	err = o.Finish(context.Background(), nil, "the-code")
	require.ErrorContains(t, err, "bad_verification_code")
	require.Empty(t, o.AccessToken)
}

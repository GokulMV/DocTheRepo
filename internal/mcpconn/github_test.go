package mcpconn

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/mcpclient"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// memStore is an in-memory Store.
type memStore struct {
	mu    sync.Mutex
	s     map[string]store.MCPServer
	oauth map[string][]byte
}

func (m *memStore) List(context.Context) ([]store.MCPServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []store.MCPServer{}
	for _, s := range m.s {
		out = append(out, s)
	}
	return out, nil
}

func (m *memStore) Get(_ context.Context, id string) (store.MCPServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.s[id]
	if !ok {
		return s, ports.ErrNotFound
	}
	return s, nil
}

func (m *memStore) Secret(context.Context, string) (string, error) { return "", nil }

func (m *memStore) OAuth(_ context.Context, id string, out any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.oauth[id]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (m *memStore) SetOAuth(_ context.Context, id string, v any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, _ := json.Marshal(v)
	m.oauth[id] = b
	var o mcpclient.OAuth
	_ = json.Unmarshal(b, &o)
	s := m.s[id]
	s.SignedIn = o.AccessToken != "" || o.RefreshToken != ""
	m.s[id] = s
	return nil
}

func (m *memStore) SetStatus(_ context.Context, id, status, lastError string, tools []store.MCPTool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.s[id]
	s.Status, s.LastError, s.Tools = status, lastError, tools
	m.s[id] = s
	return nil
}

// apps is a GitHubApps with one App whose client secret the test can change.
type apps struct {
	web    string
	secret string
}

func (a *apps) GitHubApps(context.Context) ([]store.GitHubApp, error) {
	return []store.GitHubApp{{ConnectorID: "app-1", Name: "GitHub", Web: a.web, ClientID: "Iv23liAPPCLIENT", OAuth: true}}, nil
}

func (a *apps) GitHubAppClient(_ context.Context, id string) (store.GitHubAppClient, error) {
	if id != "app-1" {
		return store.GitHubAppClient{}, ports.ErrNotFound
	}
	return store.GitHubAppClient{GitHubApp: store.GitHubApp{ConnectorID: id, Web: a.web, ClientID: "Iv23liAPPCLIENT", OAuth: true}, ClientSecret: a.secret}, nil
}

// fakeGitHub serves the token endpoint of GitHub's web application flow (errors are HTTP 200 with
// "error") and a GitHub MCP server that takes the user token it issued.
type fakeGitHub struct {
	mu        sync.Mutex
	challenge string
	refresh   map[string]bool
	live      string // the access token the MCP server accepts
	n         int
	secret    string
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	answer := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/login/oauth/access_token":
		_ = r.ParseForm()
		f := r.PostForm
		if f.Get("client_id") != "Iv23liAPPCLIENT" || f.Get("client_secret") != g.secret || f.Has("resource") {
			answer(map[string]any{"error": "incorrect_client_credentials"})
			return
		}
		issue := func() {
			g.n++
			at, rt := "ghu_"+string(rune('0'+g.n)), "ghr_"+string(rune('0'+g.n))
			g.live, g.refresh[rt] = at, true
			answer(map[string]any{"access_token": at, "expires_in": 28800, "refresh_token": rt, "refresh_token_expires_in": 15897600, "token_type": "bearer", "scope": ""})
		}
		if f.Get("grant_type") == "refresh_token" {
			if !g.refresh[f.Get("refresh_token")] {
				answer(map[string]any{"error": "bad_refresh_token"})
				return
			}
			delete(g.refresh, f.Get("refresh_token"))
			issue()
			return
		}
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if f.Get("code") != "gh-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			answer(map[string]any{"error": "bad_verification_code"})
			return
		}
		issue()
	case "/mcp/":
		if g.live == "" || r.Header.Get("Authorization") != "Bearer "+g.live {
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
		var result any = map[string]any{}
		switch msg.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "github"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "list_issues", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}}
		}
		answer(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	default:
		http.NotFound(w, r)
	}
}

func TestGitHubAppSignIn(t *testing.T) {
	g := &fakeGitHub{refresh: map[string]bool{}, secret: "app-secret"}
	srv := httptest.NewServer(g)
	defer srv.Close()
	st := &memStore{s: map[string]store.MCPServer{"m1": {ID: "m1", Name: "GitHub", URL: srv.URL + "/mcp/", Auth: "oauth", Enabled: true, MinRole: "editor",
		Config: map[string]string{ConfigGitHubApp: "app-1"}}}, oauth: map[string][]byte{}}
	a := &apps{web: srv.URL, secret: "app-secret"}
	m := &Manager{Store: st, GitHubApps: a, PublicURL: "https://hub.example"}
	ctx := context.Background()

	// Start: straight to GitHub's sign-in page with the App's client, no discovery or registration.
	link, err := m.StartSignIn(ctx, "m1", "http://ignored")
	require.NoError(t, err)
	u, _ := url.Parse(link)
	require.Equal(t, srv.URL+"/login/oauth/authorize", u.Scheme+"://"+u.Host+u.Path)
	q := u.Query()
	require.Equal(t, "Iv23liAPPCLIENT", q.Get("client_id"))
	require.Equal(t, "https://hub.example"+CallbackPath, q.Get("redirect_uri"))
	require.True(t, strings.HasPrefix(q.Get("state"), "m1."))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.False(t, q.Has("resource"))
	g.challenge = q.Get("code_challenge")

	// A forged state is refused; the real one finishes and connects.
	_, err = m.FinishSignIn(ctx, "m1.forged", "gh-code")
	require.Error(t, err)
	id, err := m.FinishSignIn(ctx, q.Get("state"), "gh-code")
	require.NoError(t, err)
	require.Equal(t, "m1", id)
	s, _ := st.Get(ctx, "m1")
	require.Equal(t, "ok", s.Status, s.LastError)
	require.Len(t, s.Tools, 1)
	var o mcpclient.OAuth
	_, _ = st.OAuth(ctx, "m1", &o)
	require.Equal(t, "ghu_1", o.AccessToken)
	require.Equal(t, "ghr_1", o.RefreshToken)

	// The token lapses: the next call refreshes with the App's current secret (it was rotated) and keeps
	// the new refresh token.
	g.mu.Lock()
	g.secret = "rotated-secret"
	g.mu.Unlock()
	a.secret = "rotated-secret"
	o.Expiry = time.Now().Add(-time.Minute)
	require.NoError(t, st.SetOAuth(ctx, "m1", &o))
	m.Forget("m1")
	s, err = m.Check(ctx, "m1")
	require.NoError(t, err)
	require.Equal(t, "ok", s.Status, s.LastError)
	_, _ = st.OAuth(ctx, "m1", &o)
	require.Equal(t, "ghu_2", o.AccessToken)
	require.Equal(t, "ghr_2", o.RefreshToken, "rotated")

	// GitHub refuses the refresh token (revoked, or older than 6 months): the connection needs sign-in again.
	o.Expiry, o.RefreshToken = time.Now().Add(-time.Minute), "ghr_1"
	require.NoError(t, st.SetOAuth(ctx, "m1", &o))
	s, err = m.Check(ctx, "m1")
	require.NoError(t, err)
	require.Equal(t, "needs_sign_in", s.Status)
	require.False(t, s.SignedIn, "the tokens are dropped")
}

func TestGitHubAppSignIn_NeedsAnApp(t *testing.T) {
	st := &memStore{s: map[string]store.MCPServer{"m1": {ID: "m1", URL: "https://api.githubcopilot.com/mcp/", Auth: "oauth",
		Config: map[string]string{ConfigGitHubApp: "gone"}}}, oauth: map[string][]byte{}}
	_, err := (&Manager{Store: st, PublicURL: "https://hub.example"}).StartSignIn(context.Background(), "m1", "")
	require.ErrorContains(t, err, "no GitHub App")
	_, err = (&Manager{Store: st, GitHubApps: &apps{}, PublicURL: "https://hub.example"}).StartSignIn(context.Background(), "m1", "")
	require.ErrorIs(t, err, ports.ErrNotFound)
}

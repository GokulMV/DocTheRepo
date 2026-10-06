package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeServer is a minimal Streamable HTTP MCP server: JSON or event-stream answers, a session id, and an
// optional bearer token it requires.
type fakeServer struct {
	mu       sync.Mutex
	token    string
	sse      bool
	sessions int
	forget   bool // answer 404 to the next request carrying a session (an expired session)
	calls    []string
	auth     []string
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://`+r.Host+`/.well-known/oauth-protected-resource/mcp", scope="read"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var msg struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &msg)
	f.calls = append(f.calls, msg.Method)
	if msg.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "" && f.forget {
		f.forget = false
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if msg.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch msg.Method {
	case "initialize":
		f.sessions++
		w.Header().Set("Mcp-Session-Id", fmt.Sprintf("s%d", f.sessions))
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake"}}
	case "tools/list":
		var p struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if p.Cursor == "" {
			result = map[string]any{"tools": []any{map[string]any{"name": "search_issues", "description": "Find issues", "inputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"readOnlyHint": true}}}, "nextCursor": "p2"}
		} else {
			result = map[string]any{"tools": []any{map[string]any{"name": "resolve_issue", "description": "Resolve", "inputSchema": map[string]any{"type": "object"}}}}
		}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "called " + p.Name + " " + string(p.Arguments)}}}
	default:
		result = map[string]any{}
	}
	resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	if f.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		// A notification first, then the response split over two data lines' worth of JSON.
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", resp)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(resp)
}

func TestClient_ListAndCall(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%v", sse), func(t *testing.T) {
			f := &fakeServer{sse: sse, token: "tok"}
			srv := httptest.NewServer(f)
			defer srv.Close()
			c := &Client{URL: srv.URL + "/mcp", Auth: Bearer("tok")}
			tools, err := c.ListTools(context.Background())
			require.NoError(t, err)
			require.Len(t, tools, 2)
			require.True(t, tools[0].ReadOnly())
			require.False(t, tools[1].ReadOnly())
			res, err := c.CallTool(context.Background(), "search_issues", json.RawMessage(`{"q":"npe"}`))
			require.NoError(t, err)
			require.Equal(t, `called search_issues {"q":"npe"}`, res.Text)
			require.Equal(t, []string{"initialize", "notifications/initialized", "tools/list", "tools/list", "tools/call"}, f.calls)
		})
	}
}

func TestClient_RestartsAnExpiredSession(t *testing.T) {
	f := &fakeServer{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := &Client{URL: srv.URL}
	_, err := c.ListTools(context.Background())
	require.NoError(t, err)
	f.forget = true
	_, err = c.CallTool(context.Background(), "search_issues", nil)
	require.NoError(t, err)
	require.Equal(t, 2, f.sessions)
}

func TestClient_AuthErrorCarriesTheChallenge(t *testing.T) {
	f := &fakeServer{token: "tok"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	err := (&Client{URL: srv.URL + "/mcp"}).Initialize(context.Background())
	var ae *AuthError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, http.StatusUnauthorized, ae.Status)
	require.Contains(t, ae.WWWAuthenticate, "resource_metadata=")
}

func TestBearer_KeepsAnExplicitScheme(t *testing.T) {
	require.Equal(t, "Bearer abc", Bearer("abc").Value)
	require.Equal(t, "ApiKey abc==", Bearer("ApiKey abc==").Value)
}

// TestOAuth_FullRoundTrip: discovery from the 401 challenge, dynamic registration, PKCE, code exchange
// with the resource indicator, then refresh.
func TestOAuth_FullRoundTrip(t *testing.T) {
	var as *httptest.Server
	var registered, exchanged, refreshed bool
	var challenge string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": as.URL, "authorization_endpoint": as.URL + "/authorize", "token_endpoint": as.URL + "/token",
			"registration_endpoint": as.URL + "/register", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		require.Equal(t, []any{"https://hub.example/api/v1/mcp/oauth/callback"}, in["redirect_uris"])
		registered = true
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-1"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "client-1", r.Form.Get("client_id"))
		require.NotEmpty(t, r.Form.Get("resource"))
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			require.Equal(t, challenge, base64.RawURLEncoding.EncodeToString(sum[:]), "PKCE verifier must match the challenge")
			require.Equal(t, "the-code", r.Form.Get("code"))
			exchanged = true
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600})
		case "refresh_token":
			require.Equal(t, "rt-1", r.Form.Get("refresh_token"))
			refreshed = true
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-2", "expires_in": 3600})
		}
	})
	as = httptest.NewServer(mux)
	defer as.Close()

	rs := http.NewServeMux()
	rs.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": "http://" + r.Host + "/mcp", "authorization_servers": []string{as.URL}, "scopes_supported": []string{"read"}})
	})
	rs.Handle("/mcp", &fakeServer{token: "at-1"})
	mcpSrv := httptest.NewServer(rs)
	defer mcpSrv.Close()

	ctx := context.Background()
	err := (&Client{URL: mcpSrv.URL + "/mcp"}).Initialize(ctx)
	var ae *AuthError
	require.ErrorAs(t, err, &ae)
	o, reg, err := Discover(ctx, nil, mcpSrv.URL+"/mcp", ae.WWWAuthenticate)
	require.NoError(t, err)
	require.Equal(t, "read", o.Scope)
	require.NoError(t, Register(ctx, nil, reg, o, "https://hub.example/api/v1/mcp/oauth/callback", "DocTheRepo Hub"))
	require.True(t, registered)
	link, err := o.Begin("id.state", "https://hub.example/api/v1/mcp/oauth/callback")
	require.NoError(t, err)
	u, _ := url.Parse(link)
	require.Equal(t, "S256", u.Query().Get("code_challenge_method"))
	require.Equal(t, mcpSrv.URL+"/mcp", u.Query().Get("resource"))
	challenge = u.Query().Get("code_challenge")
	require.Equal(t, HashState("id.state"), o.PendingState)
	require.NoError(t, o.Finish(ctx, nil, "the-code"))
	require.True(t, exchanged)
	require.Equal(t, "at-1", o.AccessToken)
	require.Empty(t, o.Verifier, "the verifier is single-use")

	c := &Client{URL: mcpSrv.URL + "/mcp", Auth: TokenFunc(func(context.Context) (string, error) { return o.AccessToken, nil })}
	_, err = c.ListTools(ctx)
	require.NoError(t, err)

	require.NoError(t, o.Refresh(ctx, nil))
	require.True(t, refreshed)
	require.Equal(t, "at-2", o.AccessToken)
	require.Equal(t, "rt-1", o.RefreshToken, "kept when the server does not rotate it")
}

func TestDiscover_FallsBackToDefaultEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	o, reg, err := Discover(context.Background(), nil, srv.URL+"/mcp", "")
	require.NoError(t, err)
	require.Equal(t, srv.URL+"/authorize", o.AuthEndpoint)
	require.Equal(t, srv.URL+"/register", reg)
}

func TestSigV4_SignsTheRequest(t *testing.T) {
	s, err := NewSigV4(context.Background(), "us-east-1", "aws-mcp", `{"access_key_id":"AKIDEXAMPLE","secret_access_key":"secret"}`)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "https://aws-mcp.us-east-1.api.aws/mcp", strings.NewReader("{}"))
	require.NoError(t, s.Authorize(context.Background(), req, []byte("{}")))
	h := req.Header.Get("Authorization")
	require.True(t, strings.HasPrefix(h, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/"), h)
	require.Contains(t, h, "/us-east-1/aws-mcp/aws4_request")
	require.NotEmpty(t, req.Header.Get("X-Amz-Date"))
}

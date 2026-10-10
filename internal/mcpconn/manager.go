// Package mcpconn runs the Hub's MCP connections: it connects to each configured server with its kind of
// sign-in, keeps the list of tools, handles the OAuth sign-in round trip, and calls tools for Ask. Only
// tools an admin allows are called (by default, the ones the server marks read-only), and only for people
// whose role meets the connection's minimum.
package mcpconn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/mcpclient"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// Store is what the manager needs from store.MCPServers.
type Store interface {
	List(ctx context.Context) ([]store.MCPServer, error)
	Get(ctx context.Context, id string) (store.MCPServer, error)
	Secret(ctx context.Context, id string) (string, error)
	OAuth(ctx context.Context, id string, out any) (bool, error)
	SetOAuth(ctx context.Context, id string, v any) error
	SetStatus(ctx context.Context, id, status, lastError string, tools []store.MCPTool) error
}

// Manager connects to MCP servers.
type Manager struct {
	Store Store
	HTTP  *http.Client
	// PublicURL is where browsers reach the Hub (for the OAuth callback).
	PublicURL string
	Version   string
	// CallTimeout bounds one tool call.
	CallTimeout time.Duration

	mu      sync.Mutex
	clients map[string]*conn
	oauthMu sync.Mutex
}

type conn struct {
	client  *mcpclient.Client
	version string // the row's settings it was built from
}

// CallbackPath is where sign-in providers send the browser back.
const CallbackPath = "/api/v1/mcp/oauth/callback"

func (m *Manager) httpClient() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// RedirectURI is the OAuth callback address; base is used when no public address is configured.
func (m *Manager) RedirectURI(base string) string {
	if m.PublicURL != "" {
		base = m.PublicURL
	}
	return strings.TrimSuffix(base, "/") + CallbackPath
}

func fingerprint(s store.MCPServer) string {
	b, _ := json.Marshal([]any{s.URL, s.Auth, s.Config, s.HasSecret, s.SignedIn})
	return string(b)
}

// Forget drops the cached connection (after an edit).
func (m *Manager) Forget(id string) {
	m.mu.Lock()
	delete(m.clients, id)
	m.mu.Unlock()
}

func (m *Manager) client(ctx context.Context, s store.MCPServer) (*mcpclient.Client, error) {
	fp := fingerprint(s)
	m.mu.Lock()
	if c, ok := m.clients[s.ID]; ok && c.version == fp {
		m.mu.Unlock()
		return c.client, nil
	}
	m.mu.Unlock()
	authz, err := m.authorizer(ctx, s)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	for k, v := range s.Config {
		if h, ok := strings.CutPrefix(k, "header:"); ok && h != "" {
			headers[h] = v
		}
	}
	c := &mcpclient.Client{URL: s.URL, HTTP: m.httpClient(), Auth: authz, Headers: headers, Name: "DocTheRepo Hub", Version: m.Version}
	m.mu.Lock()
	if m.clients == nil {
		m.clients = map[string]*conn{}
	}
	m.clients[s.ID] = &conn{client: c, version: fp}
	m.mu.Unlock()
	return c, nil
}

func (m *Manager) authorizer(ctx context.Context, s store.MCPServer) (mcpclient.Authorizer, error) {
	secret := ""
	if s.HasSecret {
		var err error
		if secret, err = m.Store.Secret(ctx, s.ID); err != nil {
			return nil, fmt.Errorf("open the stored key: %w", err)
		}
	}
	switch s.Auth {
	case "none":
		return nil, nil
	case "bearer":
		if secret == "" {
			return nil, errors.New("no token is stored")
		}
		return mcpclient.Bearer(secret), nil
	case "header":
		name := s.Config["header_name"]
		if name == "" || secret == "" {
			return nil, errors.New("the header name and value are required")
		}
		return mcpclient.Header{Name: name, Value: secret}, nil
	case "aws":
		region, service := s.Config["region"], s.Config["service"]
		if region == "" || service == "" {
			region, service = guessAWS(s.URL, region, service)
		}
		return mcpclient.NewSigV4(ctx, region, service, secret, s.Config["role_arn"], s.Config["external_id"])
	case "google":
		return mcpclient.NewGoogle(ctx, secret)
	case "oauth":
		id := s.ID
		return mcpclient.TokenFunc(func(ctx context.Context) (string, error) { return m.accessToken(ctx, id) }), nil
	}
	return nil, fmt.Errorf("unknown sign-in kind %q", s.Auth)
}

var awsHost = regexp.MustCompile(`^([a-z0-9-]+)\.([a-z]{2}-[a-z]+-\d)\.api\.aws$`)

// guessAWS reads the signing service and region from an AWS endpoint host such as
// aws-mcp.us-east-1.api.aws, keeping any value already set.
func guessAWS(raw, region, service string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil {
		return region, service
	}
	if m := awsHost.FindStringSubmatch(u.Hostname()); m != nil {
		if service == "" {
			service = m[1]
		}
		if region == "" {
			region = m[2]
		}
	}
	return region, service
}

// ErrNeedsSignIn: the connection uses OAuth and nobody has signed in yet, or the sign-in lapsed.
var ErrNeedsSignIn = errors.New("an admin needs to sign in to this connection")

func (m *Manager) accessToken(ctx context.Context, id string) (string, error) {
	m.oauthMu.Lock()
	defer m.oauthMu.Unlock()
	var o mcpclient.OAuth
	ok, err := m.Store.OAuth(ctx, id, &o)
	if err != nil {
		return "", err
	}
	if !ok || (o.AccessToken == "" && o.RefreshToken == "") {
		return "", ErrNeedsSignIn
	}
	if o.Valid() {
		return o.AccessToken, nil
	}
	if err := o.Refresh(ctx, m.httpClient()); err != nil {
		if errors.Is(err, mcpclient.ErrSignInAgain) {
			o.AccessToken, o.RefreshToken = "", ""
			_ = m.Store.SetOAuth(ctx, id, &o)
			_ = m.Store.SetStatus(ctx, id, "needs_sign_in", "the sign-in expired; sign in again", nil)
			m.Forget(id)
			return "", ErrNeedsSignIn
		}
		return "", err
	}
	if err := m.Store.SetOAuth(ctx, id, &o); err != nil {
		return "", err
	}
	return o.AccessToken, nil
}

// Check connects, lists the tools and records the outcome. It returns the updated connection.
func (m *Manager) Check(ctx context.Context, id string) (store.MCPServer, error) {
	s, err := m.Store.Get(ctx, id)
	if err != nil {
		return s, err
	}
	m.Forget(id)
	tools, err := m.listTools(ctx, s)
	switch {
	case errors.Is(err, ErrNeedsSignIn):
		_ = m.Store.SetStatus(ctx, id, "needs_sign_in", "", nil)
	case err != nil:
		var ae *mcpclient.AuthError
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized && s.Auth == "oauth" {
			_ = m.Store.SetStatus(ctx, id, "needs_sign_in", "", nil)
		} else {
			_ = m.Store.SetStatus(ctx, id, "error", explain(s, err), nil)
		}
	default:
		_ = m.Store.SetStatus(ctx, id, "ok", "", tools)
	}
	return m.Store.Get(ctx, id)
}

func (m *Manager) listTools(ctx context.Context, s store.MCPServer) ([]store.MCPTool, error) {
	c, err := m.client(ctx, s)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ts, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]store.MCPTool, 0, len(ts))
	for _, t := range ts {
		title := t.Title
		if title == "" {
			title = t.Annotations.Title
		}
		out = append(out, store.MCPTool{Name: t.Name, Title: title, Description: t.Description, InputSchema: t.InputSchema, ReadOnly: t.ReadOnly()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// explain turns a connection error into what an admin can act on.
func explain(s store.MCPServer, err error) string {
	var ae *mcpclient.AuthError
	if errors.As(err, &ae) {
		if ae.Status == http.StatusUnauthorized {
			switch s.Auth {
			case "none":
				return "the server needs sign-in: choose how it signs in (OAuth, a token or a key)"
			case "bearer", "header":
				return "the server refused the key (401): check it is current and has read access"
			case "aws":
				return "AWS refused the signature (401): check the region, service and credentials"
			}
		}
		return ae.Error()
	}
	return err.Error()
}

// StartSignIn begins the OAuth sign-in and returns the address to send the admin's browser to.
// base is the address the admin reached the Hub at, used when server.public_url is not set.
func (m *Manager) StartSignIn(ctx context.Context, id, base string) (string, error) {
	s, err := m.Store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if s.Auth != "oauth" {
		return "", errors.New("this connection does not sign in with OAuth")
	}
	redirect := m.RedirectURI(base)
	if !strings.HasPrefix(redirect, "https://") && !strings.HasPrefix(redirect, "http://localhost") && !strings.HasPrefix(redirect, "http://127.0.0.1") {
		return "", errors.New("sign-in providers only return to https addresses (or localhost): open the Hub over https, or set server.public_url")
	}
	hc := m.httpClient()
	m.oauthMu.Lock()
	defer m.oauthMu.Unlock()
	var prev mcpclient.OAuth
	had, err := m.Store.OAuth(ctx, id, &prev)
	if err != nil {
		return "", err
	}
	// Ask the server who signs people in: an unauthenticated initialize returns the challenge.
	probe := &mcpclient.Client{URL: s.URL, HTTP: hc, Name: "DocTheRepo Hub", Version: m.Version}
	wwwAuth := ""
	var ae *mcpclient.AuthError
	if err := probe.Initialize(ctx); errors.As(err, &ae) {
		wwwAuth = ae.WWWAuthenticate
	}
	o, regEndpoint, err := mcpclient.Discover(ctx, hc, s.URL, wwwAuth)
	if err != nil {
		return "", err
	}
	if v := s.Config["scope"]; v != "" {
		o.Scope = v
	}
	switch {
	case s.Config["client_id"] != "":
		// An OAuth app the admin created in the product (Google Cloud, some Atlassian setups).
		o.ClientID = s.Config["client_id"]
		if sec, _ := m.Store.Secret(ctx, id); sec != "" {
			o.ClientSecret = sec
			if o.TokenAuth == "none" {
				o.TokenAuth = "client_secret_post"
			}
		}
	case had && prev.Registered && prev.ClientID != "" && prev.TokenEndpoint == o.TokenEndpoint && prev.RedirectURI == redirect:
		o.ClientID, o.ClientSecret, o.TokenAuth, o.Registered = prev.ClientID, prev.ClientSecret, prev.TokenAuth, true
	default:
		if err := mcpclient.Register(ctx, hc, regEndpoint, o, redirect, "DocTheRepo Hub"); err != nil {
			return "", err
		}
	}
	state := id + "." + randomToken()
	authURL, err := o.Begin(state, redirect)
	if err != nil {
		return "", err
	}
	if err := m.Store.SetOAuth(ctx, id, o); err != nil {
		return "", err
	}
	return authURL, nil
}

// FinishSignIn completes the sign-in the browser comes back from. It returns the connection's id.
func (m *Manager) FinishSignIn(ctx context.Context, state, code string) (string, error) {
	id, _, ok := strings.Cut(state, ".")
	if !ok || id == "" || code == "" {
		return "", errors.New("the sign-in link is incomplete")
	}
	m.oauthMu.Lock()
	var o mcpclient.OAuth
	had, err := m.Store.OAuth(ctx, id, &o)
	if err != nil || !had || o.PendingState == "" || !constantEq(o.PendingState, mcpclient.HashState(state)) {
		m.oauthMu.Unlock()
		return "", errors.New("this sign-in was not started here or has already finished")
	}
	err = o.Finish(ctx, m.httpClient(), code)
	if serr := m.Store.SetOAuth(ctx, id, &o); err == nil {
		err = serr
	}
	m.oauthMu.Unlock()
	if err != nil {
		return id, err
	}
	_, _ = m.Check(ctx, id)
	return id, nil
}

func constantEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Tool is a tool Ask may call.
type Tool struct {
	Name        string // <connection>.<tool>, unique
	Server      string
	ServerID    string
	ToolName    string
	Description string
	Schema      string
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "_"), "_")
}

// Tools lists what someone with role may use: enabled, connected connections whose minimum role they
// meet, and their allowed tools.
func (m *Manager) Tools(ctx context.Context, role string) []Tool {
	servers, err := m.Store.List(ctx)
	if err != nil {
		return nil
	}
	var out []Tool
	for _, s := range servers {
		if !s.Enabled || s.Status != "ok" || !auth.Role(role).AtLeast(auth.Role(s.MinRole)) {
			continue
		}
		for _, t := range s.Tools {
			if !s.ToolOn(t) {
				continue
			}
			out = append(out, Tool{Name: slug(s.Name) + "." + t.Name, Server: s.Name, ServerID: s.ID, ToolName: t.Name,
				Description: firstNonEmpty(t.Description, t.Title), Schema: string(t.InputSchema)})
		}
	}
	return out
}

// Call runs a tool by its Tools name for someone with role, checking again that they may.
func (m *Manager) Call(ctx context.Context, role, name string, args json.RawMessage) (Tool, string, error) {
	var tool Tool
	for _, t := range m.Tools(ctx, role) {
		if t.Name == name {
			tool = t
			break
		}
	}
	if tool.Name == "" {
		return tool, "", fmt.Errorf("no tool named %q is available", name)
	}
	s, err := m.Store.Get(ctx, tool.ServerID)
	if err != nil {
		return tool, "", err
	}
	c, err := m.client(ctx, s)
	if err != nil {
		return tool, "", err
	}
	timeout := m.CallTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := c.CallTool(cctx, tool.ToolName, args)
	if err != nil {
		return tool, "", err
	}
	if res.IsError {
		return tool, "", fmt.Errorf("the tool reported an error: %s", clip(res.Text, 400))
	}
	return tool, res.Text, nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

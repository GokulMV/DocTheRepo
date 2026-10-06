package mcpclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// OAuth sign-in for MCP servers (the MCP authorization spec): find the authorization server from the MCP
// server's protected-resource metadata (RFC 9728), read its metadata (RFC 8414 / OpenID discovery),
// register the Hub as a client (RFC 7591) unless a client ID is given, then the authorization code flow
// with PKCE (S256) and the resource indicator (RFC 8707). Tokens refresh on their own.

// OAuth is everything the Hub keeps for one server's sign-in (stored sealed).
type OAuth struct {
	Resource      string `json:"resource"`
	Issuer        string `json:"issuer,omitempty"`
	AuthEndpoint  string `json:"authorization_endpoint"`
	TokenEndpoint string `json:"token_endpoint"`
	Scope         string `json:"scope,omitempty"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret,omitempty"`
	TokenAuth     string `json:"token_auth,omitempty"` // none | client_secret_post | client_secret_basic
	// Registered: the Hub registered this client itself (so a new address registers again).
	Registered bool `json:"registered,omitempty"`

	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`

	// A sign-in in progress: the state's hash, the PKCE verifier, where the browser comes back to.
	PendingState string    `json:"pending_state,omitempty"`
	Verifier     string    `json:"verifier,omitempty"`
	RedirectURI  string    `json:"redirect_uri,omitempty"`
	PendingUntil time.Time `json:"pending_until,omitempty"`
}

// Valid reports whether the access token can be used without refreshing.
func (o *OAuth) Valid() bool {
	return o.AccessToken != "" && (o.Expiry.IsZero() || time.Until(o.Expiry) > time.Minute)
}

type protectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type asMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

var authParam = regexp.MustCompile(`([a-zA-Z_]+)="([^"]*)"`)

// challengeParam reads one parameter of a WWW-Authenticate: Bearer challenge.
func challengeParam(header, name string) string {
	for _, m := range authParam.FindAllStringSubmatch(header, -1) {
		if strings.EqualFold(m[1], name) {
			return m[2]
		}
	}
	return ""
}

// Discover finds where to sign in for serverURL. wwwAuth is the WWW-Authenticate header of its 401 (may be
// empty). Servers without protected-resource metadata are treated as their own authorization server.
func Discover(ctx context.Context, hc *http.Client, serverURL, wwwAuth string) (*OAuth, string, error) {
	u, err := url.Parse(serverURL)
	if err != nil || u.Host == "" {
		return nil, "", errors.New("the server address is not a URL")
	}
	origin := u.Scheme + "://" + u.Host
	path := strings.TrimSuffix(u.Path, "/")
	var prm protectedResource
	candidates := []string{}
	if v := challengeParam(wwwAuth, "resource_metadata"); v != "" {
		candidates = append(candidates, v)
	}
	if path != "" {
		candidates = append(candidates, origin+"/.well-known/oauth-protected-resource"+path)
	}
	candidates = append(candidates, origin+"/.well-known/oauth-protected-resource")
	found := false
	for _, c := range candidates {
		if getJSON(ctx, hc, c, &prm) == nil && len(prm.AuthorizationServers) > 0 {
			found = true
			break
		}
	}
	issuer := origin
	if found {
		issuer = strings.TrimSuffix(prm.AuthorizationServers[0], "/")
	}
	md, err := authServerMetadata(ctx, hc, issuer)
	if err != nil {
		if found {
			return nil, "", err
		}
		// Older servers (spec 2025-03-26) without metadata use default paths on their own origin.
		md = &asMetadata{Issuer: origin, AuthorizationEndpoint: origin + "/authorize", TokenEndpoint: origin + "/token", RegistrationEndpoint: origin + "/register"}
	}
	if md.AuthorizationEndpoint == "" || md.TokenEndpoint == "" {
		return nil, "", errors.New("the sign-in server does not list its endpoints")
	}
	if len(md.CodeChallengeMethodsSupported) > 0 && !contains(md.CodeChallengeMethodsSupported, "S256") {
		return nil, "", errors.New("the sign-in server does not support PKCE (S256)")
	}
	scope := challengeParam(wwwAuth, "scope")
	if scope == "" && len(prm.ScopesSupported) > 0 {
		scope = strings.Join(prm.ScopesSupported, " ")
	}
	resource := serverURL
	if prm.Resource != "" {
		resource = prm.Resource
	}
	o := &OAuth{Resource: resource, Issuer: md.Issuer, AuthEndpoint: md.AuthorizationEndpoint, TokenEndpoint: md.TokenEndpoint, Scope: scope}
	switch {
	case contains(md.TokenEndpointAuthMethodsSupported, "none") || len(md.TokenEndpointAuthMethodsSupported) == 0:
		o.TokenAuth = "none"
	case contains(md.TokenEndpointAuthMethodsSupported, "client_secret_post"):
		o.TokenAuth = "client_secret_post"
	default:
		o.TokenAuth = "client_secret_basic"
	}
	return o, md.RegistrationEndpoint, nil
}

func authServerMetadata(ctx context.Context, hc *http.Client, issuer string) (*asMetadata, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return nil, err
	}
	origin := u.Scheme + "://" + u.Host
	path := strings.TrimSuffix(u.Path, "/")
	urls := []string{origin + "/.well-known/oauth-authorization-server" + path, origin + "/.well-known/openid-configuration" + path}
	if path != "" {
		urls = append(urls, issuer+"/.well-known/openid-configuration")
	}
	for _, x := range urls {
		var md asMetadata
		if getJSON(ctx, hc, x, &md) == nil && md.TokenEndpoint != "" {
			return &md, nil
		}
	}
	return nil, fmt.Errorf("no sign-in metadata found for %s", issuer)
}

// Register registers the Hub as a public client with the authorization server.
func Register(ctx context.Context, hc *http.Client, endpoint string, o *OAuth, redirectURI, clientName string) error {
	if endpoint == "" {
		return errors.New("this server does not let apps register themselves: create an OAuth app in the product and enter its client ID")
	}
	method := o.TokenAuth
	if method == "" {
		method = "none"
	}
	body := map[string]any{
		"client_name": clientName, "redirect_uris": []string{redirectURI},
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": method,
	}
	if o.Scope != "" {
		body["scope"] = o.Scope
	}
	var out struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthMethod   string `json:"token_endpoint_auth_method"`
	}
	if err := postJSON(ctx, hc, endpoint, body, &out); err != nil {
		return fmt.Errorf("register with the sign-in server: %w", err)
	}
	if out.ClientID == "" {
		return errors.New("the sign-in server did not return a client ID")
	}
	o.ClientID, o.ClientSecret, o.Registered = out.ClientID, out.ClientSecret, true
	if out.AuthMethod != "" {
		o.TokenAuth = out.AuthMethod
	}
	return nil
}

// Begin starts a sign-in: it returns the address to send the browser to. state must be unguessable.
func (o *OAuth) Begin(state, redirectURI string) (string, error) {
	vb := make([]byte, 32)
	if _, err := rand.Read(vb); err != nil {
		return "", err
	}
	o.Verifier = base64.RawURLEncoding.EncodeToString(vb)
	sum := sha256.Sum256([]byte(o.Verifier))
	o.PendingState = HashState(state)
	o.RedirectURI = redirectURI
	o.PendingUntil = time.Now().Add(15 * time.Minute)
	q := url.Values{
		"response_type": {"code"}, "client_id": {o.ClientID}, "redirect_uri": {redirectURI}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"resource": {o.Resource},
	}
	if o.Scope != "" {
		q.Set("scope", o.Scope)
	}
	sep := "?"
	if strings.Contains(o.AuthEndpoint, "?") {
		sep = "&"
	}
	return o.AuthEndpoint + sep + q.Encode(), nil
}

// HashState is how a pending state is stored (the state itself only travels in the browser).
func HashState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Finish exchanges the code the browser brought back for tokens.
func (o *OAuth) Finish(ctx context.Context, hc *http.Client, code string) error {
	if o.Verifier == "" || time.Now().After(o.PendingUntil) {
		return errors.New("this sign-in expired: start it again")
	}
	err := o.token(ctx, hc, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {o.RedirectURI}, "code_verifier": {o.Verifier}})
	o.PendingState, o.Verifier, o.PendingUntil = "", "", time.Time{}
	return err
}

// ErrSignInAgain: the refresh token is gone or refused; an admin must sign in again.
var ErrSignInAgain = errors.New("sign in again")

// Refresh gets a new access token with the refresh token.
func (o *OAuth) Refresh(ctx context.Context, hc *http.Client) error {
	if o.RefreshToken == "" {
		return ErrSignInAgain
	}
	err := o.token(ctx, hc, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {o.RefreshToken}})
	var te *tokenError
	if errors.As(err, &te) && (te.Code == "invalid_grant" || te.Status == http.StatusUnauthorized) {
		return ErrSignInAgain
	}
	return err
}

type tokenError struct {
	Status int
	Code   string
	Desc   string
}

func (e *tokenError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("the sign-in server refused: %s %s", e.Code, e.Desc)
	}
	return fmt.Sprintf("the sign-in server answered %d", e.Status)
}

func (o *OAuth) token(ctx context.Context, hc *http.Client, form url.Values) error {
	form.Set("resource", o.Resource)
	if o.TokenAuth != "client_secret_basic" {
		form.Set("client_id", o.ClientID)
		if o.TokenAuth == "client_secret_post" && o.ClientSecret != "" {
			form.Set("client_secret", o.ClientSecret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if o.TokenAuth == "client_secret_basic" {
		req.SetBasicAuth(url.QueryEscape(o.ClientID), url.QueryEscape(o.ClientSecret))
	}
	resp, err := client(hc).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		// Some servers answer form-encoded.
		v, _ := url.ParseQuery(string(b))
		out.AccessToken, out.RefreshToken, out.Error = v.Get("access_token"), v.Get("refresh_token"), v.Get("error")
	}
	if resp.StatusCode >= 300 || out.AccessToken == "" {
		return &tokenError{Status: resp.StatusCode, Code: out.Error, Desc: out.Description}
	}
	o.AccessToken = out.AccessToken
	if out.RefreshToken != "" {
		o.RefreshToken = out.RefreshToken
	}
	o.Expiry = time.Time{}
	if out.ExpiresIn > 0 {
		o.Expiry = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return nil
}

func client(hc *http.Client) *http.Client {
	if hc == nil {
		return http.DefaultClient
	}
	return hc
}

func getJSON(ctx context.Context, hc *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client(hc).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %d", u, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func postJSON(ctx context.Context, hc *http.Client, u string, in, out any) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client(hc).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%d %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 300)])))
	}
	return json.Unmarshal(body, out)
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

package atlassian

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Atlassian Cloud OAuth 2.0 authorization code grants ("3LO"): the operator registers one OAuth app in the
// Atlassian developer console; an admin clicks "Connect with Atlassian", approves on Atlassian's consent page
// (which also picks the site), and the Hub exchanges the code for an access token (about an hour) and a
// rotating refresh token (offline_access: every refresh returns a new one, which replaces the old). The
// cloud ID of each site the token can read comes from accessible-resources, and the REST APIs are then
// called through the API gateway: <API>/ex/confluence/<cloudid>/… and <API>/ex/jira/<cloudid>/….
// Atlassian does not document PKCE for these apps, so the code is protected by the client secret and a
// sealed, user-bound state instead.

// Endpoints are Atlassian's OAuth and API addresses (overridden in tests).
type Endpoints struct {
	Authorize string // https://auth.atlassian.com/authorize
	Token     string // https://auth.atlassian.com/oauth/token
	API       string // https://api.atlassian.com (accessible-resources and the /ex/… gateway)
}

// Cloud are the production endpoints.
var Cloud = Endpoints{Authorize: "https://auth.atlassian.com/authorize", Token: "https://auth.atlassian.com/oauth/token", API: "https://api.atlassian.com"}

// ConsoleURL is where the operator registers the OAuth app.
const ConsoleURL = "https://developer.atlassian.com/console/myapps/"

// Scopes are the read-only classic scopes each product's adapter needs, plus offline_access for a refresh
// token. Confluence: CQL content search with bodies and labels (search:confluence,
// read:confluence-content.all), the space of each page (read:confluence-space.summary). Jira: JQL search and
// issues (read:jira-work), the account's time zone from /myself (read:jira-user).
var Scopes = map[string][]string{
	"confluence": {"read:confluence-content.all", "read:confluence-space.summary", "search:confluence", "offline_access"},
	"jira":       {"read:jira-work", "read:jira-user", "offline_access"},
}

// Tokens are what the token endpoint returns.
type Tokens struct {
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
}

// Valid reports whether the access token can be used without refreshing.
func (t Tokens) Valid() bool {
	return t.AccessToken != "" && (t.Expiry.IsZero() || time.Until(t.Expiry) > time.Minute)
}

// Resource is one site an access token can read.
type Resource struct {
	ID     string   `json:"id"` // the cloud ID
	URL    string   `json:"url"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

// Serves reports whether the site was granted scopes of product (confluence, jira). A site listed without
// scopes is assumed to serve both.
func (r Resource) Serves(product string) bool {
	if len(r.Scopes) == 0 {
		return true
	}
	for _, s := range r.Scopes {
		if strings.Contains(s, product) {
			return true
		}
	}
	return false
}

// ErrSignInAgain: the refresh token expired (90 days unused), was revoked, or was already rotated away; only
// a new sign-in helps.
var ErrSignInAgain = errors.New("atlassian: sign in again")

// AuthorizeURL is the consent page to send the browser to.
func AuthorizeURL(ep Endpoints, clientID, redirectURI, state string, scopes []string) string {
	q := url.Values{
		"audience": {"api.atlassian.com"}, "client_id": {clientID}, "scope": {strings.Join(scopes, " ")},
		"redirect_uri": {redirectURI}, "state": {state}, "response_type": {"code"}, "prompt": {"consent"},
	}
	return ep.Authorize + "?" + q.Encode()
}

// Exchange trades the authorization code for tokens.
func Exchange(ctx context.Context, hc *http.Client, ep Endpoints, clientID, secret, code, redirectURI string) (Tokens, error) {
	return token(ctx, hc, ep, map[string]string{"grant_type": "authorization_code", "client_id": clientID, "client_secret": secret,
		"code": code, "redirect_uri": redirectURI})
}

// Refresh trades a refresh token for new tokens; the returned refresh token replaces the old one.
func Refresh(ctx context.Context, hc *http.Client, ep Endpoints, clientID, secret, refreshToken string) (Tokens, error) {
	t, err := token(ctx, hc, ep, map[string]string{"grant_type": "refresh_token", "client_id": clientID, "client_secret": secret,
		"refresh_token": refreshToken})
	var te *TokenError
	if errors.As(err, &te) && (te.Code == "invalid_grant" || te.Status == http.StatusForbidden) {
		return t, ErrSignInAgain
	}
	if err == nil && t.RefreshToken == "" {
		t.RefreshToken = refreshToken // a non-rotating answer keeps the current one
	}
	return t, err
}

// TokenError is a refusal from the token endpoint (never carries a token).
type TokenError struct {
	Status int
	Code   string
	Desc   string
}

func (e *TokenError) Error() string {
	if e.Status == http.StatusUnauthorized && (e.Code == "" || e.Code == "access_denied" || e.Code == "unauthorized_client") {
		return "Atlassian rejected the Hub's OAuth app: check its client ID and secret in the one-time setup"
	}
	if e.Code != "" {
		return fmt.Sprintf("Atlassian refused: %s %s", e.Code, e.Desc)
	}
	return fmt.Sprintf("Atlassian's token endpoint answered HTTP %d", e.Status)
}

func token(ctx context.Context, hc *http.Client, ep Endpoints, body map[string]string) (Tokens, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.Token, bytes.NewReader(b))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client(hc).Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("reach Atlassian: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 || out.AccessToken == "" {
		return Tokens{}, &TokenError{Status: resp.StatusCode, Code: out.Error, Desc: truncate(out.Description, 200)}
	}
	t := Tokens{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken}
	if out.ExpiresIn > 0 {
		t.Expiry = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return t, nil
}

// Resources lists the sites the access token can read (accessible-resources).
func Resources(ctx context.Context, hc *http.Client, ep Endpoints, accessToken string) ([]Resource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.API+"/oauth/token/accessible-resources", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := client(hc).Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach Atlassian: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Atlassian did not list the sites this sign-in can read (HTTP %d)", resp.StatusCode)
	}
	var out []Resource
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("Atlassian's site list: %w", err)
	}
	return out, nil
}

// TokenSource gives OAuth connectors a current access token, refreshing (and storing the rotated refresh
// token) when needed. refresh forces a new token, after the API refused the current one.
type TokenSource interface {
	Token(ctx context.Context, cc ports.ConnectorConfig, refresh bool) (string, error)
	// APIBase is the API gateway (Endpoints.API).
	APIBase() string
}

// IsOAuth reports whether a connector signs in with Atlassian OAuth (rather than an API token or PAT).
func IsOAuth(cc ports.ConnectorConfig) bool { return cc.Config["auth"] == "oauth" }

func client(hc *http.Client) *http.Client {
	if hc == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return hc
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

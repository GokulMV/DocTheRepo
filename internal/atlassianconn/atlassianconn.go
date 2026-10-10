// Package atlassianconn connects Confluence and Jira Cloud with "Connect with Atlassian" (OAuth 2.0
// authorization code grants, 3LO): it starts the sign-in, finishes it into a connector (choosing the site),
// and is the adapters' token source, refreshing access tokens and storing each rotated refresh token. A
// refresh Atlassian refuses marks the connector as needing a new sign-in, shown on the Connections page.
package atlassianconn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/atlassian"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// Connectors is what the service needs from store.Connectors.
type Connectors interface {
	Get(ctx context.Context, id string) (ports.ConnectorConfig, error)
	Create(ctx context.Context, n store.NewConnector) (string, error)
	Update(ctx context.Context, id string, p store.ConnectorPatch) error
	SetHealth(ctx context.Context, id string, syncErr error) error
}

// Apps is what the service needs from store.AtlassianApps.
type Apps interface {
	Get(ctx context.Context) (store.AtlassianApp, bool, error)
}

// Service is the Atlassian OAuth connection flow and token source.
type Service struct {
	Connectors Connectors
	Apps       Apps
	// Seal and Open protect the state carried through Atlassian's redirect (secrets.Box).
	Seal func(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	Open func(ctx context.Context, blob, aad []byte) ([]byte, error)
	// PublicURL is server.public_url; without it the callback is on the address the admin used.
	PublicURL string
	Endpoints atlassian.Endpoints // zero: atlassian.Cloud
	HTTP      *http.Client
	Now       func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex      // per connector: one refresh at a time
	cache map[string]atlassian.Tokens // access tokens by connector
	used  map[string]time.Time        // states already finished, until they expire
}

// CallbackPath is where Atlassian sends the browser back (under the Hub's public URL).
const CallbackPath = "/api/v1/atlassian/connect/callback"

// stateTTL bounds a sign-in: long enough to sign in to Atlassian, short enough that a leaked link is useless.
const stateTTL = 15 * time.Minute

var stateAAD = []byte("dth/atlassian-connect/state")

// Error codes the API returns.
const (
	CodeAppMissing = "ATLASSIAN_APP_MISSING"
	CodeSignIn     = "ATLASSIAN_SIGN_IN_REQUIRED"
)

// ErrAppMissing: the operator has not registered the OAuth app yet.
var ErrAppMissing = &ports.ValidationError{Code: CodeAppMissing,
	Message: "Atlassian sign-in is not set up on this Hub yet: an admin registers the Hub with Atlassian once (Connections → Confluence or Jira → One-time setup)"}

func signInErr() error {
	return &ports.ValidationError{Code: CodeSignIn,
		Message: "the Atlassian sign-in expired or was revoked: choose \"Sign in again\" on this connector"}
}

func (s *Service) ep() atlassian.Endpoints {
	if s.Endpoints.Token == "" {
		return atlassian.Cloud
	}
	return s.Endpoints
}

func (s *Service) hc() *http.Client {
	if s.HTTP == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return s.HTTP
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// APIBase implements atlassian.TokenSource.
func (s *Service) APIBase() string { return s.ep().API }

// RedirectURI is the callback URL to register in the Atlassian developer console. base is the address the
// admin reached the Hub at, used when server.public_url is not set.
func (s *Service) RedirectURI(base string) string {
	b := strings.TrimSuffix(s.PublicURL, "/")
	if b == "" {
		b = strings.TrimSuffix(base, "/")
	}
	return b + CallbackPath
}

func (s *Service) app(ctx context.Context) (store.AtlassianApp, error) {
	app, ok, err := s.Apps.Get(ctx)
	if err != nil {
		return app, err
	}
	if !ok || app.ClientID == "" || app.ClientSecret == "" {
		return app, ErrAppMissing
	}
	return app, nil
}

// Configured reports whether the OAuth app is set up.
func (s *Service) Configured(ctx context.Context) bool {
	_, err := s.app(ctx)
	return err == nil
}

// state travels through Atlassian's redirect, sealed: nothing half-finished is stored, and it cannot be read
// or forged. It is bound to the admin who started, expires, and works once.
type state struct {
	UserID      string    `json:"u"`
	Type        string    `json:"t"`           // confluence | jira
	ConnectorID string    `json:"c,omitempty"` // signing an existing connector in again
	Name        string    `json:"n,omitempty"`
	Keys        string    `json:"k,omitempty"` // spaces or projects
	Site        string    `json:"s,omitempty"` // the site the admin asked for (host), if any
	Redirect    string    `json:"r"`
	Nonce       string    `json:"x"`
	Expires     time.Time `json:"e"`
}

// StartRequest is what the admin asked for.
type StartRequest struct {
	Type        string // confluence | jira
	ConnectorID string // sign an existing connector in again
	Name        string
	Keys        string // spaces or projects (optional; can be added after connecting)
	Site        string // e.g. acme.atlassian.net, to choose among several sites (optional)
}

func keysField(typ string) string {
	if typ == "jira" {
		return "projects"
	}
	return "spaces"
}

func bad(msg string) error { return &ports.ValidationError{Code: "INVALID_PARAMETER", Message: msg} }

// Start returns Atlassian's consent page for userID's sign-in. base is the address the admin used.
func (s *Service) Start(ctx context.Context, userID string, req StartRequest, base string) (string, error) {
	if req.Type != "confluence" && req.Type != "jira" {
		return "", bad("type must be confluence or jira")
	}
	app, err := s.app(ctx)
	if err != nil {
		return "", err
	}
	redirect := s.RedirectURI(base)
	if !strings.HasPrefix(redirect, "https://") && !strings.HasPrefix(redirect, "http://localhost") && !strings.HasPrefix(redirect, "http://127.0.0.1") {
		return "", bad("Atlassian only returns to https addresses (or localhost): open the Hub over https, or set server.public_url")
	}
	if req.ConnectorID != "" {
		cc, err := s.Connectors.Get(ctx, req.ConnectorID)
		if err != nil {
			return "", err
		}
		if cc.Type != req.Type || !atlassian.IsOAuth(cc) {
			return "", bad("this connector does not sign in with Atlassian")
		}
	}
	if k := strings.TrimSpace(req.Keys); k != "" {
		if _, err := atlassian.Keys(req.Type, keysField(req.Type), k); err != nil {
			return "", err
		}
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	st := state{UserID: userID, Type: req.Type, ConnectorID: req.ConnectorID, Name: strings.TrimSpace(req.Name), Keys: strings.TrimSpace(req.Keys),
		Site: siteHost(req.Site), Redirect: redirect, Nonce: base64.RawURLEncoding.EncodeToString(nonce), Expires: s.now().Add(stateTTL)}
	b, _ := json.Marshal(st)
	ct, err := s.Seal(ctx, b, stateAAD)
	if err != nil {
		return "", err
	}
	return atlassian.AuthorizeURL(s.ep(), app.ClientID, redirect, base64.RawURLEncoding.EncodeToString(ct), atlassian.Scopes[req.Type]), nil
}

// siteHost reduces "https://acme.atlassian.net/wiki" or "acme" to a lower-case host.
func siteHost(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := u.Hostname()
	if !strings.Contains(h, ".") {
		h += ".atlassian.net"
	}
	return h
}

func (s *Service) openState(ctx context.Context, raw, userID string) (state, error) {
	var st state
	blob, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(blob) == 0 {
		return st, errors.New("the sign-in link is missing its state; start again from Connections")
	}
	b, err := s.Open(ctx, blob, stateAAD)
	if err != nil || json.Unmarshal(b, &st) != nil || st.Nonce == "" {
		return st, errors.New("the sign-in link's state is not valid; start again from Connections")
	}
	if s.now().After(st.Expires) {
		return st, errors.New("the sign-in took too long and expired; start again from Connections")
	}
	if st.UserID == "" || st.UserID != userID {
		return st, errors.New("this sign-in was started by another user")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used == nil {
		s.used = map[string]time.Time{}
	}
	for n, exp := range s.used {
		if s.now().After(exp) {
			delete(s.used, n)
		}
	}
	if _, done := s.used[st.Nonce]; done {
		return st, errors.New("this sign-in has already finished")
	}
	s.used[st.Nonce] = st.Expires
	return st, nil
}

// Result is what a finished sign-in did.
type Result struct {
	ConnectorID string
	Type        string
	Site        string // the site's URL; "" when the admin still has to choose one
	ChooseSite  bool
	Reconnected bool
}

// Finish completes the sign-in the browser brought back: it exchanges the code, finds the site, and creates
// the connector (or stores the new sign-in on the connector being signed in again).
func (s *Service) Finish(ctx context.Context, userID, rawState, code string) (Result, error) {
	st, err := s.openState(ctx, rawState, userID)
	if err != nil {
		return Result{}, err
	}
	if code == "" || len(code) > 4096 {
		return Result{}, errors.New("Atlassian did not return a code")
	}
	app, err := s.app(ctx)
	if err != nil {
		return Result{}, err
	}
	t, err := atlassian.Exchange(ctx, s.hc(), s.ep(), app.ClientID, app.ClientSecret, code, st.Redirect)
	if err != nil {
		return Result{}, err
	}
	if t.RefreshToken == "" {
		return Result{}, errors.New("Atlassian returned no refresh token: add the offline_access scope to the Hub's app (One-time setup)")
	}
	all, err := atlassian.Resources(ctx, s.hc(), s.ep(), t.AccessToken)
	if err != nil {
		return Result{}, err
	}
	var sites []atlassian.Resource
	for _, r := range all {
		if r.Serves(st.Type) && r.ID != "" && strings.HasPrefix(r.URL, "https://") {
			sites = append(sites, r)
		}
	}
	label := map[string]string{"confluence": "Confluence", "jira": "Jira"}[st.Type]
	if len(sites) == 0 {
		return Result{}, fmt.Errorf("this Atlassian account has no %s site the Hub's app may read: choose a site with %s on Atlassian's page, and check the app's %s scopes", label, label, label)
	}
	creds, _ := json.Marshal(t)
	res := Result{Type: st.Type}

	if st.ConnectorID != "" {
		cc, err := s.Connectors.Get(ctx, st.ConnectorID)
		if err != nil {
			return Result{}, errors.New("the connector being signed in again was not found")
		}
		cfg := clone(cc.Config)
		if id := cfg["cloud_id"]; id != "" {
			i := slices.IndexFunc(sites, func(r atlassian.Resource) bool { return r.ID == id })
			if i < 0 {
				return Result{}, fmt.Errorf("this sign-in cannot read %s: sign in with an account on that site and choose it on Atlassian's page", siteLabel(cfg))
			}
			res.Site = sites[i].URL
		} else {
			res = s.place(cfg, st, sites, res)
		}
		delete(cfg, "oauth_status")
		if res.ChooseSite {
			cfg["oauth_status"] = "choose_site"
		}
		c := string(creds)
		if err := s.Connectors.Update(ctx, cc.ID, store.ConnectorPatch{Config: &cfg, Credentials: &c}); err != nil {
			return Result{}, err
		}
		s.forget(cc.ID)
		res.ConnectorID, res.Reconnected = cc.ID, true
		return res, nil
	}

	cfg := map[string]string{"auth": "oauth"}
	if st.Keys != "" {
		cfg[keysField(st.Type)] = st.Keys
	}
	res = s.place(cfg, st, sites, res)
	if res.ChooseSite {
		cfg["oauth_status"] = "choose_site"
	}
	name := st.Name
	if name == "" {
		name = label
		if !res.ChooseSite {
			name += " (" + cfg["site_name"] + ")"
		}
	}
	// A taken name gets a number, so connecting a second site never fails on the name.
	for i := 1; i <= 20; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s %d", name, i)
		}
		id, err := s.Connectors.Create(ctx, store.NewConnector{Type: st.Type, Name: n, Mode: "poll", Config: cfg, Credentials: string(creds)})
		if err == nil {
			res.ConnectorID = id
			return res, nil
		}
	}
	return Result{}, fmt.Errorf("signed in, but the connector could not be saved (is the name %q taken?)", name)
}

// place picks the site for cfg: the one the admin named, else the only one; several leave a choice.
func (s *Service) place(cfg map[string]string, st state, sites []atlassian.Resource, res Result) Result {
	pick := -1
	if st.Site != "" {
		pick = slices.IndexFunc(sites, func(r atlassian.Resource) bool { return siteHost(r.URL) == st.Site })
	}
	if pick < 0 && len(sites) == 1 {
		pick = 0
	}
	if pick < 0 {
		choices := make([]atlassian.Resource, len(sites))
		for i, r := range sites {
			choices[i] = atlassian.Resource{ID: r.ID, URL: r.URL, Name: r.Name}
		}
		b, _ := json.Marshal(choices)
		cfg["oauth_sites"] = string(b)
		res.ChooseSite = true
		return res
	}
	setSite(cfg, st.Type, sites[pick])
	res.Site = sites[pick].URL
	return res
}

func setSite(cfg map[string]string, typ string, r atlassian.Resource) {
	base := strings.TrimRight(r.URL, "/")
	if typ == "confluence" {
		base += "/wiki"
	}
	cfg["cloud_id"], cfg["base_url"], cfg["site_name"] = r.ID, base, r.Name
	if cfg["site_name"] == "" {
		cfg["site_name"] = siteHost(r.URL)
	}
	delete(cfg, "oauth_sites")
}

func siteLabel(cfg map[string]string) string {
	if cfg["base_url"] != "" {
		return cfg["base_url"]
	}
	return "the connector's site"
}

// ChooseSite settles which of the signed-in account's sites a connector reads.
func (s *Service) ChooseSite(ctx context.Context, id, cloudID string) (string, error) {
	cc, err := s.Connectors.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !atlassian.IsOAuth(cc) {
		return "", bad("this connector does not sign in with Atlassian")
	}
	var sites []atlassian.Resource
	_ = json.Unmarshal([]byte(cc.Config["oauth_sites"]), &sites)
	i := slices.IndexFunc(sites, func(r atlassian.Resource) bool { return r.ID == cloudID })
	if i < 0 {
		return "", bad("choose one of the sites this sign-in can read")
	}
	cfg := clone(cc.Config)
	setSite(cfg, cc.Type, sites[i])
	delete(cfg, "oauth_status")
	if err := s.Connectors.Update(ctx, id, store.ConnectorPatch{Config: &cfg}); err != nil {
		return "", err
	}
	return sites[i].URL, nil
}

func clone(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Service) lock(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	if s.locks[id] == nil {
		s.locks[id] = &sync.Mutex{}
	}
	return s.locks[id]
}

// cached returns the access token last handed out for id, and whether it is still valid.
func (s *Service) cached(id string) (atlassian.Tokens, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.cache[id]
	return t, ok && t.Valid()
}

func (s *Service) remember(id string, t atlassian.Tokens) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]atlassian.Tokens{}
	}
	s.cache[id] = atlassian.Tokens{AccessToken: t.AccessToken, Expiry: t.Expiry}
}

func (s *Service) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, id)
}

// Token implements atlassian.TokenSource. The stored credentials are re-read before a refresh: the
// refresh token rotates, so the copy a long sync started with may already be spent.
func (s *Service) Token(ctx context.Context, cc ports.ConnectorConfig, refresh bool) (string, error) {
	handed, ok := s.cached(cc.ID)
	if ok && !refresh {
		return handed.AccessToken, nil
	}
	l := s.lock(cc.ID)
	l.Lock()
	defer l.Unlock()
	cur, err := s.Connectors.Get(ctx, cc.ID)
	if err != nil {
		return "", err
	}
	if cur.Config["oauth_status"] == "needs_sign_in" {
		return "", signInErr()
	}
	var t atlassian.Tokens
	_ = json.Unmarshal([]byte(cur.Credentials), &t)
	if t.Valid() && (!refresh || (handed.AccessToken != "" && t.AccessToken != handed.AccessToken)) {
		// Valid, or another caller refreshed since this one was refused.
		s.remember(cc.ID, t)
		return t.AccessToken, nil
	}
	if t.RefreshToken == "" {
		return "", s.needsSignIn(ctx, cur)
	}
	app, err := s.app(ctx)
	if err != nil {
		return "", err
	}
	nt, err := atlassian.Refresh(ctx, s.hc(), s.ep(), app.ClientID, app.ClientSecret, t.RefreshToken)
	if errors.Is(err, atlassian.ErrSignInAgain) {
		return "", s.needsSignIn(ctx, cur)
	}
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(nt)
	c := string(b)
	// The old refresh token stays usable for a short reuse interval only: store the new one before use.
	if err := s.Connectors.Update(ctx, cc.ID, store.ConnectorPatch{Credentials: &c}); err != nil {
		return "", fmt.Errorf("store the refreshed Atlassian sign-in: %w", err)
	}
	s.remember(cc.ID, nt)
	return nt.AccessToken, nil
}

// needsSignIn drops the spent tokens and marks the connector, so the Connections page offers "Sign in again".
func (s *Service) needsSignIn(ctx context.Context, cc ports.ConnectorConfig) error {
	s.forget(cc.ID)
	cfg := clone(cc.Config)
	cfg["oauth_status"] = "needs_sign_in"
	empty := "{}"
	err := signInErr()
	_ = s.Connectors.Update(context.WithoutCancel(ctx), cc.ID, store.ConnectorPatch{Config: &cfg, Credentials: &empty})
	_ = s.Connectors.SetHealth(context.WithoutCancel(ctx), cc.ID, err)
	return err
}

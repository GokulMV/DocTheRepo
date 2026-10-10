package atlassianconn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/atlassian"
	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/confluence"
	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/jira"
	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// fakeAtlassian plays auth.atlassian.com (token endpoint) and api.atlassian.com (accessible-resources and the
// /ex/… gateway). Refresh tokens rotate: each refresh spends the presented one and issues a new one.
type fakeAtlassian struct {
	*httptest.Server
	mu        sync.Mutex
	codes     map[string]bool // valid authorization codes
	refresh   map[string]bool // live refresh tokens
	access    map[string]bool // live access tokens
	sites     []atlassian.Resource
	n         int
	refreshes int
	bodies    []map[string]string
	api       []string // "<path> <authorization>"
	reject    int      // answer the next API calls with 401
}

func newFake(t *testing.T) *fakeAtlassian {
	f := &fakeAtlassian{codes: map[string]bool{"good-code": true}, refresh: map[string]bool{}, access: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAtlassian) issue() map[string]any {
	f.n++
	a, r := fmt.Sprintf("at-%d", f.n), fmt.Sprintf("rt-%d", f.n)
	f.access[a], f.refresh[r] = true, true
	return map[string]any{"access_token": a, "refresh_token": r, "expires_in": 3600, "token_type": "Bearer"}
}

func (f *fakeAtlassian) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/oauth/token":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.bodies = append(f.bodies, in)
		if r.Header.Get("Content-Type") != "application/json" || in["client_id"] != "cid-123456" || in["client_secret"] != "csecret" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"Unauthorized"}`))
			return
		}
		switch in["grant_type"] {
		case "authorization_code":
			if !f.codes[in["code"]] {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid authorization code"}`))
				return
			}
			delete(f.codes, in["code"])
		case "refresh_token":
			if !f.refresh[in["refresh_token"]] {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
				return
			}
			delete(f.refresh, in["refresh_token"])
			f.refreshes++
		}
		_ = json.NewEncoder(w).Encode(f.issue())
	case r.URL.Path == "/oauth/token/accessible-resources":
		if !f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(f.sites)
	case strings.HasPrefix(r.URL.Path, "/ex/"):
		f.api = append(f.api, r.URL.Path+" "+r.Header.Get("Authorization"))
		if f.reject > 0 || !f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			f.reject--
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/ex/confluence/cloud-a1/rest/api/content/search":
			_, _ = w.Write([]byte(`{"results":[{"id":"42","type":"page","title":"Runbook","space":{"key":"ENG"},"version":{"when":"2026-10-01T12:00:00Z"},
			  "body":{"storage":{"value":"<p>restart it</p>"}},"_links":{"webui":"/spaces/ENG/pages/42"}}],"_links":{}}`))
		case "/ex/jira/cloud-a1/rest/api/3/issue/ENG-7":
			_, _ = w.Write([]byte(`{"key":"ENG-7","fields":{"summary":"Timeouts","status":{"name":"Done","statusCategory":{"key":"done"}},"project":{"key":"ENG"}}}`))
		default:
			w.WriteHeader(404)
		}
	default:
		w.WriteHeader(404)
	}
}

func (f *fakeAtlassian) endpoints() atlassian.Endpoints {
	return atlassian.Endpoints{Authorize: f.URL + "/authorize", Token: f.URL + "/oauth/token", API: f.URL}
}

// memConnectors is store.Connectors in memory.
type memConnectors struct {
	mu     sync.Mutex
	m      map[string]ports.ConnectorConfig
	health map[string]error
	writes int // credential writes
}

func (c *memConnectors) Get(_ context.Context, id string) (ports.ConnectorConfig, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cc, ok := c.m[id]
	if !ok {
		return cc, ports.ErrNotFound
	}
	cc.Config = clone(cc.Config)
	return cc, nil
}

func (c *memConnectors) Create(_ context.Context, n store.NewConnector) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cc := range c.m {
		if cc.Name == n.Name {
			return "", errors.New("name taken")
		}
	}
	id := ports.NewID()
	c.m[id] = ports.ConnectorConfig{ID: id, Type: n.Type, Name: n.Name, Mode: n.Mode, Config: clone(n.Config), Credentials: n.Credentials}
	return id, nil
}

func (c *memConnectors) Update(_ context.Context, id string, p store.ConnectorPatch) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cc := c.m[id]
	if p.Config != nil {
		cc.Config = clone(*p.Config)
	}
	if p.Credentials != nil {
		cc.Credentials = *p.Credentials
		c.writes++
	}
	c.m[id] = cc
	return nil
}

func (c *memConnectors) SetHealth(_ context.Context, id string, err error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.health[id] = err
	return nil
}

type memApps struct{ app *store.AtlassianApp }

func (a *memApps) Get(context.Context) (store.AtlassianApp, bool, error) {
	if a.app == nil {
		return store.AtlassianApp{}, false, nil
	}
	return *a.app, true, nil
}

type env struct {
	svc   *Service
	fake  *fakeAtlassian
	conns *memConnectors
	apps  *memApps
	now   time.Time
}

func newEnv(t *testing.T) *env {
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	box := secrets.NewBox(kek)
	e := &env{fake: newFake(t), conns: &memConnectors{m: map[string]ports.ConnectorConfig{}, health: map[string]error{}},
		apps: &memApps{app: &store.AtlassianApp{ClientID: "cid-123456", ClientSecret: "csecret"}}, now: time.Now()}
	e.fake.sites = []atlassian.Resource{{ID: "cloud-a1", URL: "https://acme.atlassian.net", Name: "acme",
		Scopes: []string{"read:confluence-content.all", "search:confluence", "read:jira-work"}}}
	e.svc = &Service{Connectors: e.conns, Apps: e.apps, Seal: box.Seal, Open: box.Open, PublicURL: "https://hub.acme.example",
		Endpoints: e.fake.endpoints(), Now: func() time.Time { return e.now }}
	return e
}

// start returns the state Atlassian would bring back, checking the consent URL on the way.
func (e *env) start(t *testing.T, user string, req StartRequest) string {
	t.Helper()
	u, err := e.svc.Start(context.Background(), user, req, "http://ignored")
	require.NoError(t, err)
	pu, err := url.Parse(u)
	require.NoError(t, err)
	assert.Equal(t, e.fake.URL+"/authorize", pu.Scheme+"://"+pu.Host+pu.Path)
	q := pu.Query()
	assert.Equal(t, "api.atlassian.com", q.Get("audience"))
	assert.Equal(t, "consent", q.Get("prompt"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "cid-123456", q.Get("client_id"))
	assert.Equal(t, "https://hub.acme.example/api/v1/atlassian/connect/callback", q.Get("redirect_uri"))
	assert.Equal(t, strings.Join(atlassian.Scopes[req.Type], " "), q.Get("scope"))
	assert.Contains(t, q.Get("scope"), "offline_access")
	return q.Get("state")
}

func TestConnectCreatesConnectorForTheOnlySite(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	state := e.start(t, "u1", StartRequest{Type: "confluence", Keys: "ENG, OPS"})
	res, err := e.svc.Finish(ctx, "u1", state, "good-code")
	require.NoError(t, err)
	assert.False(t, res.ChooseSite)
	assert.Equal(t, "https://acme.atlassian.net", res.Site)
	cc, err := e.conns.Get(ctx, res.ConnectorID)
	require.NoError(t, err)
	assert.Equal(t, "Confluence (acme)", cc.Name)
	assert.Equal(t, map[string]string{"auth": "oauth", "cloud_id": "cloud-a1", "base_url": "https://acme.atlassian.net/wiki", "site_name": "acme",
		"spaces": "ENG, OPS"}, cc.Config)
	var tok atlassian.Tokens
	require.NoError(t, json.Unmarshal([]byte(cc.Credentials), &tok))
	assert.Equal(t, "rt-1", tok.RefreshToken)
	assert.Equal(t, "https://hub.acme.example/api/v1/atlassian/connect/callback", e.fake.bodies[0]["redirect_uri"])

	// The state works once.
	_, err = e.svc.Finish(ctx, "u1", state, "good-code")
	assert.ErrorContains(t, err, "already finished")
}

func TestCallbackRejectsBadStates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	for _, raw := range []string{"", "not-base64!", "Zm9yZ2Vk"} {
		_, err := e.svc.Finish(ctx, "u1", raw, "good-code")
		assert.ErrorContains(t, err, "state", raw)
	}
	state := e.start(t, "u1", StartRequest{Type: "jira"})
	_, err := e.svc.Finish(ctx, "u2", state, "good-code")
	assert.ErrorContains(t, err, "another user")
	e.now = e.now.Add(stateTTL + time.Minute)
	_, err = e.svc.Finish(ctx, "u1", state, "good-code")
	assert.ErrorContains(t, err, "expired")
	assert.Empty(t, e.conns.m, "nothing is created")
	assert.Empty(t, e.fake.bodies, "Atlassian is never asked")
}

func TestStartNeedsTheOneTimeSetup(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.apps.app = nil
	_, err := e.svc.Start(ctx, "u1", StartRequest{Type: "jira"}, "")
	var v *ports.ValidationError
	require.ErrorAs(t, err, &v)
	assert.Equal(t, CodeAppMissing, v.Code)
	assert.False(t, e.svc.Configured(ctx))

	e.apps.app = &store.AtlassianApp{ClientID: "cid-123456", ClientSecret: "csecret"}
	e.svc.PublicURL = ""
	_, err = e.svc.Start(ctx, "u1", StartRequest{Type: "jira"}, "http://hub.internal")
	assert.ErrorContains(t, err, "https")
	_, err = e.svc.Start(ctx, "u1", StartRequest{Type: "notion"}, "https://hub")
	assert.ErrorContains(t, err, "confluence or jira")
	_, err = e.svc.Start(ctx, "u1", StartRequest{Type: "jira", Keys: "bad key!"}, "https://hub")
	assert.Error(t, err)
	assert.Equal(t, "http://localhost:8080/api/v1/atlassian/connect/callback", e.svc.RedirectURI("http://localhost:8080"))
}

func TestSeveralSites(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.fake.sites = append(e.fake.sites,
		atlassian.Resource{ID: "cloud-b2", URL: "https://beta.atlassian.net", Name: "beta", Scopes: []string{"read:jira-work"}},
		atlassian.Resource{ID: "cloud-c3", URL: "https://conf-only.atlassian.net", Name: "c", Scopes: []string{"read:confluence-content.all"}})

	// The site the admin named is chosen.
	res, err := e.svc.Finish(ctx, "u1", e.start(t, "u1", StartRequest{Type: "jira", Site: "https://beta.atlassian.net/jira"}), "good-code")
	require.NoError(t, err)
	assert.Equal(t, "https://beta.atlassian.net", res.Site)
	cc, _ := e.conns.Get(ctx, res.ConnectorID)
	assert.Equal(t, "cloud-b2", cc.Config["cloud_id"])
	assert.Equal(t, "https://beta.atlassian.net", cc.Config["base_url"])

	// Otherwise the admin chooses among the Jira sites (not the Confluence-only one).
	e.fake.codes["code-2"] = true
	res, err = e.svc.Finish(ctx, "u1", e.start(t, "u1", StartRequest{Type: "jira"}), "code-2")
	require.NoError(t, err)
	require.True(t, res.ChooseSite)
	cc, _ = e.conns.Get(ctx, res.ConnectorID)
	assert.Equal(t, "Jira", cc.Name)
	assert.Equal(t, "choose_site", cc.Config["oauth_status"])
	var choices []atlassian.Resource
	require.NoError(t, json.Unmarshal([]byte(cc.Config["oauth_sites"]), &choices))
	assert.Equal(t, []string{"cloud-a1", "cloud-b2"}, []string{choices[0].ID, choices[1].ID})
	_, err = (&jira.Source{OAuth: e.svc}).Fetch(ctx, cc, "https://acme.atlassian.net/browse/ENG-7")
	assert.ErrorContains(t, err, "choose which Atlassian site")

	_, err = e.svc.ChooseSite(ctx, res.ConnectorID, "cloud-c3")
	assert.Error(t, err)
	site, err := e.svc.ChooseSite(ctx, res.ConnectorID, "cloud-a1")
	require.NoError(t, err)
	assert.Equal(t, "https://acme.atlassian.net", site)
	cc, _ = e.conns.Get(ctx, res.ConnectorID)
	assert.Equal(t, "cloud-a1", cc.Config["cloud_id"])
	assert.NotContains(t, cc.Config, "oauth_sites")
	assert.NotContains(t, cc.Config, "oauth_status")
}

func TestNoUsableSite(t *testing.T) {
	e := newEnv(t)
	e.fake.sites = []atlassian.Resource{{ID: "cloud-b2", URL: "https://beta.atlassian.net", Scopes: []string{"read:jira-work"}}}
	_, err := e.svc.Finish(context.Background(), "u1", e.start(t, "u1", StartRequest{Type: "confluence"}), "good-code")
	assert.ErrorContains(t, err, "no Confluence site")
}

// connected returns a Confluence connector signed in through the flow.
func (e *env) connected(t *testing.T, typ string) ports.ConnectorConfig {
	t.Helper()
	e.fake.mu.Lock()
	code := fmt.Sprintf("code-%s-%d", typ, len(e.conns.m))
	e.fake.codes[code] = true
	e.fake.mu.Unlock()
	res, err := e.svc.Finish(context.Background(), "u1", e.start(t, "u1", StartRequest{Type: typ, Keys: "ENG"}), code)
	require.NoError(t, err)
	cc, err := e.conns.Get(context.Background(), res.ConnectorID)
	require.NoError(t, err)
	return cc
}

func TestAdaptersUseTheGatewayWithABearerToken(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	cc := e.connected(t, "confluence")
	var docs []ports.KnowledgeDoc
	err := (&confluence.Source{OAuth: e.svc}).Changed(ctx, cc, "ENG", "", func(d []ports.KnowledgeDoc, _ string) error {
		docs = append(docs, d...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, "https://acme.atlassian.net/wiki/spaces/ENG/pages/42", docs[0].URL, "links point at the site, not the gateway")
	assert.Equal(t, []string{"/ex/confluence/cloud-a1/rest/api/content/search Bearer at-1"}, e.fake.api)
	assert.True(t, (&confluence.Source{OAuth: e.svc}).Owns(cc, "https://acme.atlassian.net/wiki/spaces/ENG/pages/42/Runbook"))

	jcc := e.connected(t, "jira")
	e.fake.api = nil
	doc, err := (&jira.Source{OAuth: e.svc}).Fetch(ctx, jcc, "https://acme.atlassian.net/browse/ENG-7")
	require.NoError(t, err)
	assert.Equal(t, "https://acme.atlassian.net/browse/ENG-7", doc.URL)
	assert.Equal(t, []string{"/ex/jira/cloud-a1/rest/api/3/issue/ENG-7 Bearer at-2"}, e.fake.api)
}

func TestRefreshRotatesAndPersists(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	cc := e.connected(t, "jira")
	writes := e.conns.writes

	// An expired access token is refreshed; the rotated refresh token is stored before use.
	e.now = e.now.Add(2 * time.Hour)
	var stored atlassian.Tokens
	_ = json.Unmarshal([]byte(cc.Credentials), &stored)
	stored.Expiry = time.Now().Add(-time.Minute)
	b, _ := json.Marshal(stored)
	expired := string(b)
	require.NoError(t, e.conns.Update(ctx, cc.ID, store.ConnectorPatch{Credentials: &expired}))
	tok, err := e.svc.Token(ctx, cc, false)
	require.NoError(t, err)
	assert.Equal(t, "at-2", tok)
	cur, _ := e.conns.Get(ctx, cc.ID)
	_ = json.Unmarshal([]byte(cur.Credentials), &stored)
	assert.Equal(t, "rt-2", stored.RefreshToken)
	assert.Equal(t, writes+2, e.conns.writes)

	// Cached: no new refresh. The API refusing the token forces one, using the stored (rotated) refresh
	// token although cc still holds the original.
	tok, err = e.svc.Token(ctx, cc, false)
	require.NoError(t, err)
	assert.Equal(t, "at-2", tok)
	assert.Equal(t, 1, e.fake.refreshes)
	e.fake.reject = 1
	_, err = (&jira.Source{OAuth: e.svc}).Fetch(ctx, cc, "https://acme.atlassian.net/browse/ENG-7")
	require.NoError(t, err)
	assert.Equal(t, 2, e.fake.refreshes)
	cur, _ = e.conns.Get(ctx, cc.ID)
	_ = json.Unmarshal([]byte(cur.Credentials), &stored)
	assert.Equal(t, "rt-3", stored.RefreshToken)
	assert.Equal(t, "Bearer at-3", strings.SplitN(e.fake.api[len(e.fake.api)-1], " ", 2)[1])
}

func TestRefusedRefreshNeedsSignInAgain(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	cc := e.connected(t, "confluence")
	e.fake.mu.Lock()
	e.fake.refresh = map[string]bool{} // revoked, or unused for 90 days
	e.fake.mu.Unlock()
	_, err := e.svc.Token(ctx, cc, true)
	var v *ports.ValidationError
	require.ErrorAs(t, err, &v)
	assert.Equal(t, CodeSignIn, v.Code)
	cur, _ := e.conns.Get(ctx, cc.ID)
	assert.Equal(t, "needs_sign_in", cur.Config["oauth_status"])
	assert.Equal(t, "{}", cur.Credentials, "spent tokens are dropped")
	require.ErrorAs(t, e.conns.health[cc.ID], &v)
	assert.Contains(t, v.Message, "Sign in again")

	// Later calls fail at once, without asking Atlassian.
	bodies := len(e.fake.bodies)
	err = (&confluence.Source{OAuth: e.svc}).Changed(ctx, cur, "ENG", "", func([]ports.KnowledgeDoc, string) error { return nil })
	require.ErrorAs(t, err, &v)
	assert.Equal(t, CodeSignIn, v.Code)
	assert.Equal(t, bodies, len(e.fake.bodies))

	// Signing in again restores the connector in place.
	e.fake.codes["code-2"] = true
	res, err := e.svc.Finish(ctx, "u1", e.start(t, "u1", StartRequest{Type: "confluence", ConnectorID: cc.ID}), "code-2")
	require.NoError(t, err)
	assert.True(t, res.Reconnected)
	assert.Equal(t, cc.ID, res.ConnectorID)
	cur, _ = e.conns.Get(ctx, cc.ID)
	assert.NotContains(t, cur.Config, "oauth_status")
	assert.Equal(t, "ENG", cur.Config["spaces"])
	tok, err := e.svc.Token(ctx, cur, false)
	require.NoError(t, err)
	assert.Equal(t, "at-2", tok)

	// A sign-in for another site does not replace it.
	e.fake.sites = []atlassian.Resource{{ID: "cloud-z9", URL: "https://other.atlassian.net", Name: "other"}}
	e.fake.codes["code-3"] = true
	_, err = e.svc.Finish(ctx, "u1", e.start(t, "u1", StartRequest{Type: "confluence", ConnectorID: cc.ID}), "code-3")
	assert.ErrorContains(t, err, "cannot read https://acme.atlassian.net/wiki")
}

func TestTokenErrorsNeverCarrySecrets(t *testing.T) {
	e := newEnv(t)
	e.apps.app.ClientSecret = "wrong-secret"
	_, err := e.svc.Finish(context.Background(), "u1", e.start(t, "u1", StartRequest{Type: "jira"}), "good-code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client ID and secret")
	assert.NotContains(t, err.Error(), "wrong-secret")
}

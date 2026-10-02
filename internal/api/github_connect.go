package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// GitHubConnectDeps are the collaborators of the one-click GitHub connection (GitHub App manifest flow).
type GitHubConnectDeps struct {
	Auth       *auth.Service
	Connectors *store.Connectors
	// Seal and Open protect the flow's state between the redirects (secrets.Box), so nothing half-finished
	// is stored and the state cannot be forged or read.
	Seal      func(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	Open      func(ctx context.Context, blob, aad []byte) ([]byte, error)
	PublicURL string
	// InvalidateHost drops a cached adapter after the installation id arrives.
	InvalidateHost func(id string)
	HTTP           *http.Client
	// SealKeys opens the sealed private key of an existing App; RequireSealed refuses a plain one.
	SealKeys      *store.SealKeys
	RequireSealed bool
}

var ghStateAAD = []byte("dth/github-connect/state")

// ghState travels through GitHub's redirects.
type ghState struct {
	UserID      string    `json:"u"`
	ConnectorID string    `json:"c"`
	Web         string    `json:"w"` // https://github.com or the GitHub Enterprise Server URL
	API         string    `json:"a"` // its REST API base, ending in /
	Name        string    `json:"n,omitempty"`
	Expires     time.Time `json:"e"`
}

// GitHubConnectRoutes mounts /github/connect…: start (returns the manifest form to post), callback (GitHub
// created the app: store its credentials as a connector, then send the user to install it), and setup
// (installed: record the installation and return to the UI).
func GitHubConnectRoutes(d GitHubConnectDeps) func(chi.Router) {
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	seal := func(ctx context.Context, st ghState) (string, error) {
		b, _ := json.Marshal(st)
		ct, err := d.Seal(ctx, b, ghStateAAD)
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(ct), nil
	}
	open := func(r *http.Request) (ghState, error) {
		var st ghState
		raw, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("state"))
		if err != nil || len(raw) == 0 {
			return st, errors.New("the link is missing its state; start again from Connectors")
		}
		b, err := d.Open(r.Context(), raw, ghStateAAD)
		if err != nil || json.Unmarshal(b, &st) != nil {
			return st, errors.New("the link's state is not valid; start again from Connectors")
		}
		if time.Now().After(st.Expires) {
			return st, errors.New("the link expired; start again from Connectors")
		}
		if p := auth.FromContext(r.Context()); p == nil || p.UserID != st.UserID {
			return st, errors.New("this link was started by another user")
		}
		return st, nil
	}
	back := func(w http.ResponseWriter, r *http.Request, q url.Values) {
		http.Redirect(w, r, "/connectors?"+q.Encode(), http.StatusFound)
	}
	fail := func(w http.ResponseWriter, r *http.Request, err error) {
		back(w, r, url.Values{"github_error": {err.Error()}})
	}
	public := strings.TrimSuffix(d.PublicURL, "/")

	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			// start builds the manifest form for org (optional) on github.com or a GitHub Enterprise Server.
			start := func(r *http.Request, org, base, name string) (action, manifest string, webhook bool, err error) {
				if public == "" {
					return "", "", false, errBadParam("set DTH_PUBLIC_URL so GitHub can send you back to this Hub")
				}
				web, api, err := githubURLs(base)
				if err != nil {
					return "", "", false, errBadParam(err.Error())
				}
				if org != "" && !ghLogin.MatchString(org) {
					return "", "", false, errBadParam("org must be a GitHub organization login")
				}
				p := auth.FromContext(r.Context())
				st := ghState{UserID: p.UserID, ConnectorID: ports.NewID(), Web: web, API: api, Name: strings.TrimSpace(name), Expires: time.Now().Add(30 * time.Minute)}
				state, err := seal(r.Context(), st)
				if err != nil {
					return "", "", false, err
				}
				webhook = Reachable(public)
				m := map[string]any{
					"name":            appName(public, org),
					"url":             public,
					"redirect_url":    public + "/api/v1/github/connect/callback",
					"setup_url":       public + "/api/v1/github/connect/setup",
					"setup_on_update": true,
					"public":          false,
					"default_permissions": map[string]string{
						"contents": "write", "pull_requests": "write", "metadata": "read", "statuses": "read", "checks": "read", "members": "read",
					},
				}
				if webhook {
					m["hook_attributes"] = map[string]any{"url": public + "/hooks/github/" + st.ConnectorID, "active": true}
					m["default_events"] = []string{"push", "pull_request", "pull_request_review"}
				}
				mj, _ := json.Marshal(m)
				action = web + "/settings/apps/new"
				if org != "" {
					action = web + "/organizations/" + url.PathEscape(org) + "/settings/apps/new"
				}
				_ = d.Auth.Audit(r.Context(), p, "github.connect.start", "connector", st.ConnectorID, map[string]any{"org": org, "web": web}, clientIP(r))
				return action + "?state=" + url.QueryEscape(state), string(mj), webhook, nil
			}
			r.Post("/github/connect", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Org     string `json:"org"`
					BaseURL string `json:"base_url"` // GitHub Enterprise Server, e.g. https://ghe.example.com
					Name    string `json:"name"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					WriteErr(w, r, err)
					return
				}
				action, manifest, webhook, err := start(r, in.Org, in.BaseURL, in.Name)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, map[string]any{"action": action, "manifest": manifest, "webhooks": webhook})
			})
			// The browser lands here from "Connect with GitHub": a page that posts the manifest to GitHub. It
			// has its own CSP (the app's allows form posts only to itself): one nonce'd script, and form posts
			// only to that GitHub host.
			r.Get("/github/connect/start", func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				action, manifest, _, err := start(r, q.Get("org"), q.Get("base_url"), q.Get("name"))
				if err != nil {
					fail(w, r, err)
					return
				}
				u, _ := url.Parse(action)
				nonce := make([]byte, 16)
				_, _ = rand.Read(nonce)
				n := base64.StdEncoding.EncodeToString(nonce)
				h := w.Header()
				h.Set("Content-Type", "text/html; charset=utf-8")
				h.Set("Cache-Control", "no-store")
				h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+n+"'; style-src 'unsafe-inline'; form-action "+u.Scheme+"://"+u.Host+"; base-uri 'none'; frame-ancestors 'none'")
				w.WriteHeader(http.StatusOK)
				_ = connectPage.Execute(w, map[string]string{"Action": action, "Manifest": manifest, "Nonce": n})
			})

			// existing connects a GitHub App the user already has, from its App ID and a private key.
			r.Post("/github/connect/existing", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					AppID         string `json:"app_id"`
					PrivateKey    string `json:"private_key"` // sealed (connector.credentials)
					BaseURL       string `json:"base_url"`
					Name          string `json:"name"`
					Account       string `json:"account"` // which installation, when the App has several
					WebhookSecret string `json:"webhook_secret"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					WriteErr(w, r, err)
					return
				}
				if !validAppID(in.AppID) {
					WriteErr(w, r, errBadParam("app_id is the number shown as \"App ID\" on the App's settings page"))
					return
				}
				if err := openSealed(r.Context(), d.SealKeys, d.RequireSealed, &in.PrivateKey, secrets.PurposeConnectorCreds); err != nil {
					WriteErr(w, r, err)
					return
				}
				if err := openSealed(r.Context(), d.SealKeys, d.RequireSealed, &in.WebhookSecret, secrets.PurposeConnectorWebhook); err != nil {
					WriteErr(w, r, err)
					return
				}
				if strings.TrimSpace(in.PrivateKey) == "" {
					WriteErr(w, r, errBadParam("private_key is required: generate one on the App's settings page"))
					return
				}
				web, apiBase, err := githubURLs(in.BaseURL)
				if err != nil {
					WriteErr(w, r, errBadParam(err.Error()))
					return
				}
				appID := strings.TrimSpace(in.AppID)
				app, insts, err := inspectApp(r.Context(), d.HTTP, apiBase, appID, in.PrivateKey)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				inst, choices := pickInstallation(insts, in.Account)
				if inst == nil && len(choices) > 1 {
					WriteError(w, r, http.StatusConflict, "CHOOSE_INSTALLATION", "This App is installed on several accounts; choose one.",
						map[string]any{"choices": choices})
					return
				}
				cfg := map[string]string{"app_id": appID, "app_slug": app.Slug, "owner": app.Owner.Login}
				if apiBase != "https://api.github.com/" {
					cfg["base_url"] = apiBase
				}
				if inst != nil {
					cfg["installation_id"] = fmt.Sprint(inst.ID)
				}
				mode := "poll" // GitHub keeps sending events to wherever the App's webhook points; poll unless told otherwise
				if in.WebhookSecret != "" {
					mode = "both"
				}
				name := strings.TrimSpace(in.Name)
				if name == "" {
					name = "GitHub (" + app.Owner.Login + ")"
				}
				id, err := d.Connectors.Create(r.Context(), store.NewConnector{Type: "github", Name: name, Mode: mode, Config: cfg,
					Credentials: in.PrivateKey, WebhookSecret: in.WebhookSecret})
				if err != nil {
					WriteError(w, r, http.StatusConflict, "CONFLICT", fmt.Sprintf("could not save the connector (is the name %q taken?)", name), nil)
					return
				}
				if d.SealKeys != nil {
					_ = d.SealKeys.SetConnectorCredsHint(r.Context(), id, in.PrivateKey)
				}
				_ = d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "github.connect.existing", "connector", id,
					map[string]any{"app_id": appID, "slug": app.Slug, "owner": app.Owner.Login, "installed": inst != nil}, clientIP(r))
				out := map[string]any{"connector_id": id, "app_slug": app.Slug, "app_name": app.Name, "owner": app.Owner.Login, "installed": inst != nil}
				if inst == nil {
					out["install_url"] = installURL(web, app.Slug)
				} else {
					out["account"] = inst.Account.Login
				}
				WriteJSON(w, http.StatusCreated, out)
			})

			// refresh looks again for the App's installation (after the user installed it on GitHub).
			r.Post("/github/connect/existing/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				cc, err := d.Connectors.GetAny(r.Context(), id)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				if cc.Type != "github" || cc.Config["app_id"] == "" {
					WriteErr(w, r, errBadParam("this connector is not a GitHub App"))
					return
				}
				apiBase := cc.Config["base_url"]
				if apiBase == "" {
					apiBase = "https://api.github.com/"
				}
				web := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(apiBase, "/"), "/api/v3"), "/")
				if apiBase == "https://api.github.com/" {
					web = "https://github.com"
				}
				var in struct {
					Account string `json:"account"`
				}
				_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in)
				app, insts, err := inspectApp(r.Context(), d.HTTP, apiBase, cc.Config["app_id"], cc.Credentials)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				inst, choices := pickInstallation(insts, in.Account)
				if inst == nil {
					if len(choices) > 1 {
						WriteError(w, r, http.StatusConflict, "CHOOSE_INSTALLATION", "This App is installed on several accounts; choose one.",
							map[string]any{"choices": choices})
						return
					}
					WriteJSON(w, http.StatusOK, map[string]any{"installed": false, "install_url": installURL(web, app.Slug)})
					return
				}
				cfg := map[string]string{}
				for k, v := range cc.Config {
					cfg[k] = v
				}
				cfg["installation_id"] = fmt.Sprint(inst.ID)
				if err := d.Connectors.Update(r.Context(), id, store.ConnectorPatch{Config: &cfg}); err != nil {
					WriteErr(w, r, err)
					return
				}
				if d.InvalidateHost != nil {
					d.InvalidateHost(id)
				}
				WriteJSON(w, http.StatusOK, map[string]any{"installed": true, "account": inst.Account.Login})
			})

			// GitHub sends the browser here after "Create GitHub App" with a one-time code.
			r.Get("/github/connect/callback", func(w http.ResponseWriter, r *http.Request) {
				st, err := open(r)
				if err != nil {
					fail(w, r, err)
					return
				}
				code := r.URL.Query().Get("code")
				if code == "" || !ghCode.MatchString(code) {
					fail(w, r, errors.New("GitHub did not return a code"))
					return
				}
				app, err := convertManifest(r.Context(), d.HTTP, st.API, code)
				if err != nil {
					fail(w, r, err)
					return
				}
				name := st.Name
				if name == "" {
					name = "GitHub (" + app.Owner.Login + ")"
				}
				mode := "webhook"
				if app.WebhookSecret == "" {
					mode = "poll" // the Hub is not reachable from GitHub (localhost): poll instead
				}
				cfg := map[string]string{"app_id": fmt.Sprint(app.ID), "app_slug": app.Slug, "owner": app.Owner.Login}
				if st.API != "https://api.github.com/" {
					cfg["base_url"] = st.API
				}
				id, err := d.Connectors.Create(r.Context(), store.NewConnector{ID: st.ConnectorID, Type: "github", Name: name, Mode: mode,
					Config: cfg, Credentials: app.PEM, WebhookSecret: app.WebhookSecret})
				if err != nil {
					fail(w, r, fmt.Errorf("the GitHub App %q was created but could not be saved (is the name %q taken?)", app.Slug, name))
					return
				}
				_ = d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "github.connect.app", "connector", id,
					map[string]any{"app_id": app.ID, "slug": app.Slug, "owner": app.Owner.Login}, clientIP(r))
				st.Expires = time.Now().Add(30 * time.Minute)
				state, err := seal(r.Context(), st)
				if err != nil {
					fail(w, r, err)
					return
				}
				install := st.Web + "/apps/" + url.PathEscape(app.Slug) + "/installations/new"
				if st.Web != "https://github.com" {
					install = st.Web + "/github-apps/" + url.PathEscape(app.Slug) + "/installations/new"
				}
				http.Redirect(w, r, install+"?state="+url.QueryEscape(state), http.StatusFound)
			})

			// GitHub sends the browser here after the app is installed (or its repositories change).
			r.Get("/github/connect/setup", func(w http.ResponseWriter, r *http.Request) {
				inst := r.URL.Query().Get("installation_id")
				if !ghDigits.MatchString(inst) {
					fail(w, r, errors.New("GitHub did not return an installation"))
					return
				}
				st, err := open(r)
				if err != nil {
					// A later "Configure" on GitHub has no state of ours; the connector already has its installation.
					back(w, r, url.Values{"github": {"updated"}})
					return
				}
				cc, err := d.Connectors.Get(r.Context(), st.ConnectorID)
				if err != nil {
					fail(w, r, errors.New("the GitHub connector was not found; start again"))
					return
				}
				cfg := map[string]string{}
				for k, v := range cc.Config {
					cfg[k] = v
				}
				cfg["installation_id"] = inst
				if err := d.Connectors.Update(r.Context(), st.ConnectorID, store.ConnectorPatch{Config: &cfg}); err != nil {
					fail(w, r, err)
					return
				}
				if d.InvalidateHost != nil {
					d.InvalidateHost(st.ConnectorID)
				}
				_ = d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "github.connect.installed", "connector", st.ConnectorID,
					map[string]any{"installation_id": inst}, clientIP(r))
				back(w, r, url.Values{"github": {"connected"}, "connector": {st.ConnectorID}})
			})
		})
	}
}

var connectPage = template.Must(template.New("connect").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>Connecting to GitHub…</title></head>
<body style="font-family:system-ui,sans-serif;display:grid;place-items:center;height:90vh;color:#334155">
<form id="f" method="post" action="{{.Action}}"><input type="hidden" name="manifest" value="{{.Manifest}}">
<p>Taking you to GitHub to create the DocTheRepo app…</p><noscript><button type="submit">Continue to GitHub</button></noscript></form>
<script nonce="{{.Nonce}}">document.getElementById('f').submit()</script></body></html>`))

var (
	ghLogin  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	ghCode   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	ghDigits = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// githubURLs returns the web and API bases for github.com (empty input) or a GitHub Enterprise Server URL.
func githubURLs(base string) (web, api string, err error) {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	if base == "" || base == "https://github.com" {
		return "https://github.com", "https://api.github.com/", nil
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", "", errors.New("base_url must be your GitHub Enterprise Server URL, e.g. https://ghe.example.com")
	}
	web = strings.TrimSuffix(strings.TrimSuffix(u.Scheme+"://"+u.Host+u.Path, "/api/v3"), "/")
	return web, web + "/api/v3/", nil
}

// Reachable reports whether GitHub can deliver webhooks to the public URL (not localhost or a private IP).
func Reachable(public string) bool {
	u, err := url.Parse(public)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") {
		return false
	}
	if ip := net.ParseIP(h); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return false
	}
	return true
}

// appName is a unique, valid GitHub App name (at most 34 characters); the user can change it on GitHub.
func appName(public, org string) string {
	who := org
	if who == "" {
		if u, err := url.Parse(public); err == nil {
			who = strings.Split(u.Hostname(), ".")[0]
		}
	}
	// GitHub App names are unique across all of GitHub (an uninstalled App keeps its name until deleted),
	// so every connect gets a random suffix. Hyphens, not spaces: GitHub keeps only the part of a manifest
	// name before the first space, which made every connect ask for the same, taken, "DocTheRepo".
	who = strings.Trim(nonSlug.ReplaceAllString(who, "-"), "-")
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	n := "DocTheRepo"
	if who != "" {
		n += "-" + who
	}
	if len(n) > 27 { // GitHub's limit is 34; leave room for "-" and six hex digits
		n = strings.TrimRight(n[:27], "-")
	}
	return n + "-" + hex.EncodeToString(b)
}

var nonSlug = regexp.MustCompile(`[^A-Za-z0-9-]+`)

type ghApp struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// convertManifest exchanges the one-time code for the new app's credentials.
func convertManifest(ctx context.Context, c *http.Client, api, code string) (ghApp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"app-manifests/"+url.PathEscape(code)+"/conversions", nil)
	if err != nil {
		return ghApp{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return ghApp{}, fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return ghApp{}, fmt.Errorf("GitHub refused the code (HTTP %d); it is single-use and expires after an hour, so start again", resp.StatusCode)
	}
	var app ghApp
	if err := json.Unmarshal(body, &app); err != nil || app.ID == 0 || app.PEM == "" {
		return ghApp{}, errors.New("GitHub's answer had no app credentials")
	}
	return app, nil
}

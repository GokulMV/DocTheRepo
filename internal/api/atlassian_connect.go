package api

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/atlassian"
	"github.com/GokulMV/DocTheRepo/internal/atlassianconn"
	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// AtlassianDeps serve "Connect with Atlassian" for Confluence and Jira Cloud.
type AtlassianDeps struct {
	Auth    *auth.Service
	Apps    *store.AtlassianApps
	Service *atlassianconn.Service
	// SealKeys opens the client secret the browser sealed; RequireSealed refuses a plain one.
	SealKeys      *store.SealKeys
	RequireSealed bool
}

// AtlassianRoutes mounts /atlassian/… (admins, like creating connectors): the one-time OAuth app setup,
// starting a sign-in, Atlassian's callback, and choosing the site when the sign-in covers several.
func AtlassianRoutes(d AtlassianDeps) func(chi.Router) {
	audit := func(r *http.Request, action, typ, id string, details any) {
		if d.Auth != nil {
			_ = d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), action, typ, id, details, clientIP(r))
		}
	}
	back := func(w http.ResponseWriter, r *http.Request, q url.Values) {
		http.Redirect(w, r, "/connectors?"+q.Encode(), http.StatusFound)
	}
	app := func(w http.ResponseWriter, r *http.Request) {
		a, ok, err := d.Apps.Get(r.Context())
		if err != nil {
			WriteErr(w, r, err)
			return
		}
		out := map[string]any{
			"configured":   ok,
			"callback_url": d.Service.RedirectURI(requestOrigin(r)),
			"console_url":  atlassian.ConsoleURL,
			"scopes":       atlassian.Scopes,
		}
		if ok {
			out["client_id"], out["updated_at"] = a.ClientID, a.UpdatedAt.UTC().Format(time.RFC3339)
		}
		WriteJSON(w, http.StatusOK, out) // never the secret
	}

	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			r.Get("/atlassian/oauth-app", app)
			r.Put("/atlassian/oauth-app", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					ClientID     string `json:"client_id"`
					ClientSecret string `json:"client_secret"` // sealed; empty keeps the stored one
				}
				if err := decodeJSON(w, r, &in); err != nil {
					fail(w, r, err)
					return
				}
				in.ClientID = strings.TrimSpace(in.ClientID)
				if !atlassianClientID.MatchString(in.ClientID) {
					fail(w, r, errBadParam("client_id is the Client ID on the app's Settings page in the Atlassian developer console"))
					return
				}
				if err := openSealed(r.Context(), d.SealKeys, d.RequireSealed, &in.ClientSecret, secrets.PurposeAtlassianSecret); err != nil {
					fail(w, r, err)
					return
				}
				_, had, err := d.Apps.Get(r.Context())
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				if !had && strings.TrimSpace(in.ClientSecret) == "" {
					fail(w, r, errBadParam("client_secret is required: copy the Secret from the app's Settings page"))
					return
				}
				if err := d.Apps.Set(r.Context(), in.ClientID, strings.TrimSpace(in.ClientSecret)); err != nil {
					WriteErr(w, r, err)
					return
				}
				audit(r, "atlassian.oauth_app.update", "app_setting", "atlassian_oauth_app", map[string]any{"client_id": in.ClientID, "secret_changed": in.ClientSecret != ""})
				app(w, r)
			})
			r.Delete("/atlassian/oauth-app", func(w http.ResponseWriter, r *http.Request) {
				if err := d.Apps.Delete(r.Context()); err != nil {
					WriteErr(w, r, err)
					return
				}
				audit(r, "atlassian.oauth_app.delete", "app_setting", "atlassian_oauth_app", nil)
				w.WriteHeader(http.StatusNoContent)
			})

			// connect returns Atlassian's consent page for the browser to go to.
			r.Post("/atlassian/connect", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Type        string `json:"type"`
					ConnectorID string `json:"connector_id"`
					Name        string `json:"name"`
					Keys        string `json:"keys"`
					Site        string `json:"site"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					fail(w, r, err)
					return
				}
				p := auth.FromContext(r.Context())
				u, err := d.Service.Start(r.Context(), p.UserID, atlassianconn.StartRequest{Type: in.Type, ConnectorID: in.ConnectorID,
					Name: in.Name, Keys: in.Keys, Site: in.Site}, requestOrigin(r))
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				audit(r, "atlassian.connect.start", "connector", in.ConnectorID, map[string]any{"type": in.Type, "site": in.Site})
				WriteJSON(w, http.StatusOK, map[string]string{"url": u})
			})

			// Atlassian sends the browser back here (a top-level GET, so the session cookie comes along); the
			// sealed state ties it to the sign-in this admin started.
			r.Get("/atlassian/connect/callback", func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if e := q.Get("error"); e != "" {
					msg := "Atlassian did not grant access"
					if e == "access_denied" {
						msg = "access was not granted on Atlassian's page"
					}
					back(w, r, url.Values{"atlassian_error": {msg}})
					return
				}
				p := auth.FromContext(r.Context())
				res, err := d.Service.Finish(r.Context(), p.UserID, q.Get("state"), q.Get("code"))
				if err != nil {
					var v *ports.ValidationError
					msg := err.Error()
					if errors.As(err, &v) {
						msg = v.Message
					}
					back(w, r, url.Values{"atlassian_error": {msg}})
					return
				}
				action := "atlassian.connect"
				if res.Reconnected {
					action = "atlassian.reconnect"
				}
				audit(r, action, "connector", res.ConnectorID, map[string]any{"type": res.Type, "site": res.Site, "choose_site": res.ChooseSite})
				status := "connected"
				if res.ChooseSite {
					status = "choose_site"
				}
				back(w, r, url.Values{"atlassian": {status}, "connector": {res.ConnectorID}})
			})

			r.Post("/atlassian/connectors/{id}/site", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					CloudID string `json:"cloud_id"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					fail(w, r, err)
					return
				}
				id := chi.URLParam(r, "id")
				site, err := d.Service.ChooseSite(r.Context(), id, in.CloudID)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				audit(r, "atlassian.site", "connector", id, map[string]string{"site": site})
				WriteJSON(w, http.StatusOK, map[string]string{"id": id, "site": site})
			})
		})
	}
}

var atlassianClientID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// atlassianManaged are the config keys "Connect with Atlassian" owns on a connector; edits keep them.
var atlassianManaged = []string{"auth", "cloud_id", "base_url", "site_name", "oauth_status", "oauth_sites"}

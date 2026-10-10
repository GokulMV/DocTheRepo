package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/mcpconn"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// MCPDeps serves MCP connections: other products' MCP servers Ask may call.
type MCPDeps struct {
	Auth          *auth.Service
	Store         *store.MCPServers
	Manager       *mcpconn.Manager
	SealKeys      *store.SealKeys
	RequireSealed bool
}

type mcpHandlers struct{ d MCPDeps }

// MCPRoutes mounts /mcp/* (admins).
func MCPRoutes(d MCPDeps) func(chi.Router) {
	h := &mcpHandlers{d: d}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			r.Get("/mcp/servers", h.list)
			r.Post("/mcp/servers", h.create)
			r.Patch("/mcp/servers/{id}", h.patch)
			r.Delete("/mcp/servers/{id}", h.remove)
			r.Post("/mcp/servers/{id}/check", h.check)
			r.Post("/mcp/servers/{id}/sign-in", h.signIn)
			// The sign-in provider sends the admin's browser back here (a top-level GET, so the session
			// cookie comes along); the state ties it to the sign-in this Hub started.
			r.Get("/mcp/oauth/callback", h.callback)
		})
	}
}

var mcpAuthKinds = []string{"none", "bearer", "header", "oauth", "aws", "google"}

func (h *mcpHandlers) audit(r *http.Request, action, id string, details any) {
	if h.d.Auth != nil {
		_ = h.d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), action, "mcp_server", id, details, clientIP(r))
	}
}

func validMCPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errBadParam("url must be an http(s) address of an MCP server")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && !strings.HasSuffix(u.Hostname(), ".internal") && !strings.HasSuffix(u.Hostname(), ".svc") {
		return errBadParam("use https for servers outside your network")
	}
	if u.Hostname() == "169.254.169.254" || u.Hostname() == "metadata.google.internal" {
		return errBadParam("that address is a cloud metadata endpoint, not an MCP server")
	}
	return nil
}

func validRole(r string) bool {
	return r == "" || slices.Contains([]string{"viewer", "editor", "admin", "owner"}, r)
}

func (h *mcpHandlers) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.d.Store.List(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	// github_apps: the Hub's GitHub Apps, and whether each can sign in to the GitHub MCP server (oauth).
	apps, err := h.d.Manager.ListGitHubApps(r.Context())
	if err != nil {
		apps = []store.GitHubApp{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "redirect_uri": h.d.Manager.RedirectURI(requestOrigin(r)), "github_apps": apps})
}

// checkGitHubApp refuses a github_app setting that cannot sign in: not OAuth, or an App without a client.
func (h *mcpHandlers) checkGitHubApp(ctx context.Context, auth string, cfg map[string]string) error {
	app := cfg[mcpconn.ConfigGitHubApp]
	if app == "" {
		return nil
	}
	if auth != "oauth" {
		return errBadParam("github_app is for connections that sign in with OAuth")
	}
	if _, err := h.d.Manager.GitHubAppClient(ctx, app); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return errBadParam("github_app must be a connected GitHub App (Connectors → GitHub)")
		}
		return err
	}
	return nil
}

func (h *mcpHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string            `json:"name"`
		URL        string            `json:"url"`
		CatalogKey string            `json:"catalog_key"`
		Auth       string            `json:"auth"`
		Config     map[string]string `json:"config"`
		Secret     string            `json:"secret"`
		MinRole    string            `json:"min_role"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	in.Name, in.URL = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL)
	if in.Name == "" {
		fail(w, r, errBadParam("name is required"))
		return
	}
	if err := validMCPURL(in.URL); err != nil {
		fail(w, r, err)
		return
	}
	if in.Auth == "" {
		in.Auth = "none"
	}
	if !slices.Contains(mcpAuthKinds, in.Auth) || !validRole(in.MinRole) {
		fail(w, r, errBadParam("auth must be none, bearer, header, oauth, aws or google; min_role a role"))
		return
	}
	if err := h.checkGitHubApp(r.Context(), in.Auth, in.Config); err != nil {
		fail(w, r, err)
		return
	}
	if err := openSealed(r.Context(), h.d.SealKeys, h.d.RequireSealed, &in.Secret, secrets.PurposeMCPSecret); err != nil {
		fail(w, r, err)
		return
	}
	id, err := h.d.Store.Create(r.Context(), store.NewMCPServer{Name: in.Name, URL: in.URL, CatalogKey: in.CatalogKey, Auth: in.Auth,
		Config: in.Config, Secret: in.Secret, MinRole: in.MinRole})
	if err != nil {
		WriteError(w, r, http.StatusConflict, "CONFLICT", "could not add the connection (is the name taken?)", nil)
		return
	}
	h.audit(r, "mcp_server.create", id, map[string]any{"name": in.Name, "url": in.URL, "auth": in.Auth, "github_app": in.Config[mcpconn.ConfigGitHubApp]})
	s, err := h.afterChange(r.Context(), id)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, s)
}

// afterChange connects right away, except for OAuth connections nobody has signed in to yet.
func (h *mcpHandlers) afterChange(ctx context.Context, id string) (store.MCPServer, error) {
	s, err := h.d.Store.Get(ctx, id)
	if err != nil {
		return s, err
	}
	h.d.Manager.Forget(id)
	if !s.Enabled || (s.Auth == "oauth" && !s.SignedIn) {
		if s.Auth == "oauth" && !s.SignedIn {
			_ = h.d.Store.SetStatus(ctx, id, "needs_sign_in", "", nil)
			return h.d.Store.Get(ctx, id)
		}
		return s, nil
	}
	return h.d.Manager.Check(ctx, id)
}

func (h *mcpHandlers) patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in store.MCPPatch
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.URL != nil {
		if err := validMCPURL(*in.URL); err != nil {
			fail(w, r, err)
			return
		}
	}
	if in.MinRole != nil && !validRole(*in.MinRole) {
		fail(w, r, errBadParam("min_role must be viewer, editor, admin or owner"))
		return
	}
	if in.Config != nil && (*in.Config)[mcpconn.ConfigGitHubApp] != "" {
		cur, err := h.d.Store.Get(r.Context(), id)
		if err != nil {
			WriteErr(w, r, err)
			return
		}
		if err := h.checkGitHubApp(r.Context(), cur.Auth, *in.Config); err != nil {
			fail(w, r, err)
			return
		}
	}
	if err := openSealed(r.Context(), h.d.SealKeys, h.d.RequireSealed, in.Secret, secrets.PurposeMCPSecret); err != nil {
		fail(w, r, err)
		return
	}
	if err := h.d.Store.Update(r.Context(), id, in); err != nil {
		WriteErr(w, r, err)
		return
	}
	h.audit(r, "mcp_server.update", id, map[string]any{"name": in.Name, "url": in.URL, "enabled": in.Enabled, "min_role": in.MinRole,
		"secret_changed": in.Secret != nil, "tools_changed": in.ToolChoices != nil})
	reconnect := in.URL != nil || in.Secret != nil || in.Config != nil || (in.Enabled != nil && *in.Enabled)
	var s store.MCPServer
	var err error
	if reconnect {
		s, err = h.afterChange(r.Context(), id)
	} else {
		s, err = h.d.Store.Get(r.Context(), id)
	}
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, s)
}

func (h *mcpHandlers) remove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.d.Store.Delete(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	h.d.Manager.Forget(id)
	h.audit(r, "mcp_server.delete", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *mcpHandlers) check(w http.ResponseWriter, r *http.Request) {
	s, err := h.d.Manager.Check(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, s)
}

func (h *mcpHandlers) signIn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u, err := h.d.Manager.StartSignIn(r.Context(), id, requestOrigin(r))
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			WriteErr(w, r, err)
			return
		}
		WriteErr(w, r, &ports.ValidationError{Code: "SIGN_IN_FAILED", Message: err.Error()})
		return
	}
	h.audit(r, "mcp_server.sign_in_start", id, nil)
	WriteJSON(w, http.StatusOK, map[string]string{"url": u})
}

func (h *mcpHandlers) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back := url.Values{}
	if e := q.Get("error"); e != "" {
		back.Set("mcp_error", strings.TrimSpace(e+" "+q.Get("error_description")))
		http.Redirect(w, r, "/connectors?"+back.Encode(), http.StatusFound)
		return
	}
	id, err := h.d.Manager.FinishSignIn(r.Context(), q.Get("state"), q.Get("code"))
	if id != "" {
		back.Set("mcp", id)
	}
	if err != nil {
		back.Set("mcp_error", err.Error())
	} else {
		h.audit(r, "mcp_server.sign_in", id, nil)
	}
	http.Redirect(w, r, "/connectors?"+back.Encode(), http.StatusFound)
}

// requestOrigin is the address the browser used to reach the Hub.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = strings.TrimSpace(strings.Split(fh, ",")[0])
	}
	return scheme + "://" + host
}

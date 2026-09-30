package api

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/observability"
)

const oidcStateCookie = "dth_oidc"

type authHandlers struct {
	svc    *auth.Service
	oidc   *auth.OIDC
	secure bool
	// loginLimit throttles password attempts per client IP.
	mu         sync.Mutex
	loginLimit map[string]*rate.Limiter
}

func (h *authHandlers) routes(r chi.Router) {
	r.Get("/auth/config", h.config)
	r.Get("/auth/login", h.login)
	r.Get("/auth/callback", h.callback)
	r.Post("/auth/local/login", h.localLogin)
	r.Post("/auth/logout", h.logout)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(auth.RoleViewer))
		r.Get("/me", h.me)
		r.Get("/tokens", h.listTokens)
		r.Post("/tokens", h.createToken)
		r.Delete("/tokens/{id}", h.deleteToken)
	})
	r.Group(func(r chi.Router) {
		r.Use(requireRole(auth.RoleAdmin))
		r.Get("/users", h.listUsers)
		r.Patch("/users/{id}", h.updateUser)
		r.Get("/users/{id}/repo-access", h.getRepoAccess)
		r.Put("/users/{id}/repo-access", h.setRepoAccess)
		r.Get("/audit", h.listAudit)
	})
}

func (h *authHandlers) setSession(w http.ResponseWriter, s auth.Session) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: s.ID, Path: "/", Expires: s.ExpiresAt, HttpOnly: true,
		Secure: h.secure, SameSite: http.SameSiteLaxMode})
}

// safeReturn only allows same-site relative paths (no open redirects).
func safeReturn(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return "/"
	}
	return p
}

// config tells the login page which sign-in methods exist (public).
func (h *authHandlers) config(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"mode": h.svc.Config().Mode, "sso": h.oidc != nil, "password": h.svc.Config().Mode == "local"})
}

func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		WriteError(w, r, http.StatusNotFound, "OIDC_NOT_CONFIGURED", "single sign-on is not configured; use local login", nil)
		return
	}
	ls := auth.NewLoginState(safeReturn(r.URL.Query().Get("return")))
	b, _ := json.Marshal(ls)
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: base64.RawURLEncoding.EncodeToString(b), Path: "/api/v1/auth",
		MaxAge: 600, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, h.oidc.AuthURL(ls), http.StatusFound)
}

func (h *authHandlers) callback(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		WriteError(w, r, http.StatusNotFound, "OIDC_NOT_CONFIGURED", "single sign-on is not configured", nil)
		return
	}
	c, err := r.Cookie(oidcStateCookie)
	var ls auth.LoginState
	if err == nil {
		b, derr := base64.RawURLEncoding.DecodeString(c.Value)
		if derr != nil || json.Unmarshal(b, &ls) != nil {
			err = derr
		}
	}
	if err != nil || ls.State == "" || subtle.ConstantTimeCompare([]byte(ls.State), []byte(r.URL.Query().Get("state"))) != 1 {
		WriteError(w, r, http.StatusBadRequest, "LOGIN_STATE_INVALID", "login expired or was started elsewhere; sign in again", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Path: "/api/v1/auth", MaxAge: -1, HttpOnly: true, Secure: h.secure})
	if e := r.URL.Query().Get("error"); e != "" {
		WriteError(w, r, http.StatusUnauthorized, "IDP_ERROR", "the identity provider returned "+e, nil)
		return
	}
	claims, err := h.oidc.Exchange(r.Context(), r.URL.Query().Get("code"), ls)
	if err != nil {
		observability.Logger(r.Context()).Warn("oidc callback failed", "err", err)
		WriteError(w, r, http.StatusUnauthorized, "OIDC_FAILED", "single sign-on failed; try again", nil)
		return
	}
	u, err := h.svc.UpsertOIDCUser(r.Context(), claims)
	if err != nil {
		fail(w, r, err)
		return
	}
	s, err := h.svc.CreateSession(r.Context(), u.ID, clientIP(r), r.UserAgent())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.setSession(w, s)
	_ = h.svc.Audit(r.Context(), &auth.Principal{UserID: u.ID}, "auth.login", "user", u.ID, map[string]string{"method": "oidc"}, clientIP(r))
	http.Redirect(w, r, ls.Return, http.StatusFound)
}

func (h *authHandlers) allowLogin(ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loginLimit == nil {
		h.loginLimit = map[string]*rate.Limiter{}
	}
	l, ok := h.loginLimit[ip]
	if !ok {
		l = rate.NewLimiter(rate.Every(6*time.Second), 5)
		h.loginLimit[ip] = l
	}
	return l.Allow()
}

func (h *authHandlers) localLogin(w http.ResponseWriter, r *http.Request) {
	if h.svc.Config().Mode != "local" {
		WriteError(w, r, http.StatusNotFound, "LOCAL_LOGIN_DISABLED", "password login is disabled; use single sign-on", nil)
		return
	}
	if !h.allowLogin(clientIP(r)) {
		w.Header().Set("Retry-After", "6")
		WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "too many login attempts", nil)
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	u, err := h.svc.PasswordLogin(r.Context(), in.Email, in.Password)
	if err != nil {
		fail(w, r, err)
		return
	}
	s, err := h.svc.CreateSession(r.Context(), u.ID, clientIP(r), r.UserAgent())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	h.setSession(w, s)
	_ = h.svc.Audit(r.Context(), &auth.Principal{UserID: u.ID}, "auth.login", "user", u.ID, map[string]string{"method": "password"}, clientIP(r))
	WriteJSON(w, http.StatusOK, map[string]any{"user": u, "csrf_token": s.CSRF})
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	_ = h.svc.EndSession(r.Context(), auth.FromContext(r.Context()))
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: h.secure})
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandlers) me(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	scope, err := h.svc.RepoScope(r.Context(), p)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	ids := scope.IDs
	if ids == nil {
		ids = []string{}
	}
	out := map[string]any{"id": p.UserID, "email": p.Email, "name": p.Name, "role": p.Role,
		"repo_access": map[string]any{"all": scope.All, "repo_ids": ids}}
	if p.Via == "session" {
		out["csrf_token"] = p.CSRF
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *authHandlers) listTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := h.svc.ListTokens(r.Context(), auth.FromContext(r.Context()).UserID)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": ts})
}

func (h *authHandlers) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string `json:"name"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.ExpiresInDays < 0 || in.ExpiresInDays > 3650 {
		fail(w, r, errBadParam("expires_in_days must be between 0 (never) and 3650"))
		return
	}
	p := auth.FromContext(r.Context())
	raw, t, err := h.svc.CreateToken(r.Context(), p.UserID, in.Name, time.Duration(in.ExpiresInDays)*24*time.Hour)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.svc.Audit(r.Context(), p, "token.create", "api_token", t.ID, map[string]string{"name": t.Name}, clientIP(r))
	WriteJSON(w, http.StatusCreated, map[string]any{"token": raw, "id": t.ID, "name": t.Name, "expires_at": t.ExpiresAt})
}

func (h *authHandlers) deleteToken(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteToken(r.Context(), p.UserID, id); err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.svc.Audit(r.Context(), p, "token.delete", "api_token", id, nil, clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	var cur struct {
		After string `json:"a"`
	}
	limit, err := pageParams(r, &cur)
	if err != nil {
		fail(w, r, err)
		return
	}
	us, err := h.svc.ListUsers(r.Context(), cur.After, limit)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newPage(us, limit, func(u auth.User) any { return map[string]string{"a": u.Email} }))
}

func (h *authHandlers) updateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	var role *auth.Role
	if in.Role != nil {
		rl, err := auth.ParseRole(*in.Role)
		if err != nil {
			fail(w, r, errBadParam(err.Error()))
			return
		}
		role = &rl
	}
	p := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")
	u, err := h.svc.UpdateUser(r.Context(), p, id, role, in.Disabled)
	if err != nil {
		fail(w, r, err)
		return
	}
	_ = h.svc.Audit(r.Context(), p, "user.update", "user", id, in, clientIP(r))
	WriteJSON(w, http.StatusOK, u)
}

func (h *authHandlers) getRepoAccess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.svc.GetUser(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	grants, err := h.svc.DirectRepoAccess(r.Context(), id)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": grants})
}

func (h *authHandlers) setRepoAccess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RepoIDs []string `json:"repo_ids"`
		Level   string   `json:"level"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	p := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")
	if _, err := h.svc.GetUser(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	if err := h.svc.SetRepoAccess(r.Context(), id, in.RepoIDs, in.Level); err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.svc.Audit(r.Context(), p, "user.repo_access", "user", id, in, clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandlers) listAudit(w http.ResponseWriter, r *http.Request) {
	var cur struct {
		Before *time.Time `json:"b"`
	}
	limit, err := pageParams(r, &cur)
	if err != nil {
		fail(w, r, err)
		return
	}
	since := time.Time{}
	if v := r.URL.Query().Get("since"); v != "" {
		if since, err = time.Parse(time.RFC3339, v); err != nil {
			fail(w, r, errBadParam("since must be RFC 3339"))
			return
		}
	}
	es, err := h.svc.ListAudit(r.Context(), r.URL.Query().Get("actor"), r.URL.Query().Get("action"), since, cur.Before, limit)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newPage(es, limit, func(e auth.AuditEntry) any { return map[string]any{"b": e.At} }))
}

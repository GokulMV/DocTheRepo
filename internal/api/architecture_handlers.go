package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// ArchitectureDeps are the collaborators of the Architecture tab.
type ArchitectureDeps struct {
	Auth  *auth.Service
	Store *store.ArchitectureStore
	// Scan re-reads a repository's authored diagrams (ingest.ArchitectureSync.Scan).
	Scan func(ctx context.Context, repoID string) (ingest.ScanResult, error)
}

// diagramCSP runs an authored diagram as an opaque, sandboxed document: its scripts work, but it cannot
// read the Hub's cookies, call the API, or load anything from the network.
const diagramCSP = "sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox allow-downloads; default-src 'none'; " +
	"script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; " +
	"connect-src 'none'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'"

// ArchitectureRoutes mounts /architecture.
func ArchitectureRoutes(d ArchitectureDeps) func(chi.Router) {
	repoAllowed := func(w http.ResponseWriter, r *http.Request, repoID string) bool {
		sc, err := scopeOf(r, d.Auth)
		if err != nil {
			WriteErr(w, r, err)
			return false
		}
		if !allows(sc, repoID) {
			WriteErr(w, r, ports.ErrNotFound) // not "forbidden": a hidden repository does not exist for the caller
			return false
		}
		return true
	}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/architecture", func(w http.ResponseWriter, r *http.Request) {
				sc, err := scopeOf(r, d.Auth)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				items, err := d.Store.Summaries(r.Context(), sc)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, map[string]any{"items": items})
			})
			r.Get("/architecture/repos/{id}", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				if !repoAllowed(w, r, id) {
					return
				}
				sc, _ := scopeOf(r, d.Auth)
				a, err := d.Store.Repo(r.Context(), id, sc)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, a)
			})
			r.Get("/architecture/diagrams/{id}", func(w http.ResponseWriter, r *http.Request) {
				repoID, html, err := d.Store.DiagramHTML(r.Context(), chi.URLParam(r, "id"))
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				if !repoAllowed(w, r, repoID) {
					return
				}
				h := w.Header()
				h.Set("Content-Type", "text/html; charset=utf-8")
				h.Set("Content-Security-Policy", diagramCSP)
				h.Set("X-Frame-Options", "SAMEORIGIN")
				h.Set("Cache-Control", "private, max-age=60")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(html))
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleEditor))
			r.Post("/architecture/repos/{id}/scan", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				if !repoAllowed(w, r, id) {
					return
				}
				if d.Scan == nil {
					WriteErr(w, r, ports.ErrNotFound)
					return
				}
				res, err := d.Scan(r.Context(), id)
				var he *ingest.HostError
				if errors.As(err, &he) && !errors.Is(err, ports.ErrNotFound) {
					// Say what the git host answered: a generic "retry shortly" hides a revoked token or
					// a missing permission.
					loggerFor(r).Warn("architecture scan: git host", "repo_id", id, "err", err)
					WriteError(w, r, http.StatusBadGateway, "GIT_HOST_ERROR", "Could not "+he.Error(), nil)
					return
				}
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				_ = d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "architecture.scan", "repo", id, res, clientIP(r))
				WriteJSON(w, http.StatusOK, res)
			})
		})
	}
}

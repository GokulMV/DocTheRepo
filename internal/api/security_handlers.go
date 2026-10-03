package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/core/security"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// SecurityDeps serve the Security page: kryptonite-style scans of repositories and fixes on request.
type SecurityDeps struct {
	Auth  *auth.Service
	Store *store.Security
	Queue Enqueuer
	// Plan reads what a scan would attack and estimates it, with no model call.
	Plan func(ctx context.Context, repoID string, modules []string) (security.Plan, error)
}

// SecurityRoutes mounts /security. Findings describe weaknesses, so editors and above see them, and only for
// repositories they can read. Scanning reads code and calls the security route; fixing opens a pull request,
// and only for findings someone selected.
func SecurityRoutes(d SecurityDeps) func(chi.Router) {
	allowed := func(w http.ResponseWriter, r *http.Request, repoID string) bool {
		sc, err := scopeOf(r, d.Auth)
		if err != nil {
			WriteErr(w, r, err)
			return false
		}
		if !allows(sc, repoID) {
			WriteErr(w, r, ports.ErrNotFound)
			return false
		}
		return true
	}
	modules := func(w http.ResponseWriter, r *http.Request, picked []string) ([]string, bool) {
		if len(picked) == 0 {
			for _, m := range security.Modules() {
				if m.Default {
					picked = append(picked, m.Name)
				}
			}
		}
		seen := map[string]bool{}
		var out []string
		for _, name := range picked {
			m, ok := security.ModuleByName(name)
			if !ok {
				WriteError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "unknown module "+name, nil)
				return nil, false
			}
			if !m.Static {
				WriteError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", name+" cannot run on code alone: "+m.Reason, nil)
				return nil, false
			}
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
		return out, true
	}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleEditor))
			r.Get("/security/modules", func(w http.ResponseWriter, r *http.Request) {
				WriteJSON(w, http.StatusOK, map[string]any{"items": security.Modules(),
					"source": map[string]string{"name": "kryptonite", "url": "https://github.com/levitasOrg/kryptonite", "license": "MIT"}})
			})
			r.Get("/security", func(w http.ResponseWriter, r *http.Request) {
				sc, err := scopeOf(r, d.Auth)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				scans, err := d.Store.LatestScans(r.Context(), sc.All, sc.RepoIDs)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, map[string]any{"items": scans})
			})
			r.Get("/security/repos/{id}/scans", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				if !allowed(w, r, id) {
					return
				}
				scans, err := d.Store.RepoScans(r.Context(), id, 20)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, map[string]any{"items": scans})
			})
			r.Get("/security/scans/{id}", func(w http.ResponseWriter, r *http.Request) {
				sc, err := d.Store.GetScan(r.Context(), chi.URLParam(r, "id"))
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				if !allowed(w, r, sc.RepoID) {
					return
				}
				WriteJSON(w, http.StatusOK, sc)
			})
			r.Post("/security/repos/{id}/plan", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				if !allowed(w, r, id) {
					return
				}
				var in struct {
					Modules []string `json:"modules"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					fail(w, r, err)
					return
				}
				ms, ok := modules(w, r, in.Modules)
				if !ok {
					return
				}
				p, err := d.Plan(r.Context(), id, ms)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, p)
			})
			r.Post("/security/repos/{id}/scans", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				if !allowed(w, r, id) {
					return
				}
				var in struct {
					Modules []string `json:"modules"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					fail(w, r, err)
					return
				}
				ms, ok := modules(w, r, in.Modules)
				if !ok {
					return
				}
				p := auth.FromContext(r.Context())
				scanID, err := d.Store.CreateScan(r.Context(), id, ms, p.UserID)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				job, _, err := d.Queue.Enqueue(r.Context(), ports.NewJob{Type: security.JobScan, RepoID: id, SerialKey: "security:" + id,
					CorrelationID: correlationFor(r), MaxAttempts: 1, Payload: security.ScanPayload{ScanID: scanID, RepoID: id, Modules: ms}})
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				_ = d.Store.SetScanJob(r.Context(), scanID, job.ID)
				_ = d.Auth.Audit(r.Context(), p, "security.scan", "repo", id, map[string]any{"modules": ms}, clientIP(r))
				WriteJSON(w, http.StatusAccepted, map[string]string{"scan_id": scanID, "job_id": job.ID})
			})
			r.Post("/security/repos/{id}/fix", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				if !allowed(w, r, id) {
					return
				}
				var in struct {
					FindingIDs []string `json:"finding_ids"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					fail(w, r, err)
					return
				}
				if len(in.FindingIDs) == 0 || len(in.FindingIDs) > 25 {
					WriteError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "select between 1 and 25 findings to fix", nil)
					return
				}
				queued, err := d.Store.QueueFixes(r.Context(), id, in.FindingIDs)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				if len(queued) == 0 {
					WriteError(w, r, http.StatusConflict, "NOTHING_TO_FIX", "none of the selected findings can be fixed now (rejected, or already in a pull request)", nil)
					return
				}
				p := auth.FromContext(r.Context())
				job, _, err := d.Queue.Enqueue(r.Context(), ports.NewJob{Type: security.JobFix, RepoID: id, SerialKey: "security:" + id,
					CorrelationID: correlationFor(r), MaxAttempts: 1, Payload: security.FixPayload{RepoID: id, FindingIDs: queued}})
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				_ = d.Auth.Audit(r.Context(), p, "security.fix", "repo", id, map[string]any{"findings": queued}, clientIP(r))
				WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "queued": len(queued)})
			})
		})
	}
}

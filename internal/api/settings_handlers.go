package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/settings"
)

// SettingsPolicy limits which secret references pasted settings may resolve on the Hub. The CLI resolves
// references on the caller's machine; here they would read the Hub's own environment and files, so every
// source is off unless listed.
type SettingsPolicy struct {
	// Sources are the enabled schemes (env, file, vault, gopass, awssm, gcpsm).
	Sources []string
	// EnvPrefix is required on ${env:…} names (default DTH_SECRET_).
	EnvPrefix string
	// FileRoot is the only directory ${file:…} may read; file is disabled without it.
	FileRoot string
}

func (p SettingsPolicy) resolver() *settings.Resolver {
	allowed := map[string]bool{}
	for _, s := range p.Sources {
		if s == "file" && p.FileRoot == "" {
			continue
		}
		allowed[s] = true
	}
	prefix := p.EnvPrefix
	if prefix == "" {
		prefix = "DTH_SECRET_"
	}
	return &settings.Resolver{Allowed: allowed, EnvPrefix: prefix, FileRoot: p.FileRoot}
}

// settingsRoutes mounts /settings/apply and /settings/export. They act through the same API as the UI,
// in process and as the caller (their session or token), so roles, validation and audit are unchanged.
func settingsRoutes(policy SettingsPolicy, root func() http.Handler) func(chi.Router) {
	inner := func(r *http.Request) *settings.HTTPAPI {
		h := http.Header{}
		for _, k := range []string{"Authorization", "Cookie", CSRFHeader} {
			if v := r.Header.Get(k); v != "" {
				h.Set(k, v)
			}
		}
		h.Set("X-Forwarded-For", clientIP(r))
		h.Set("X-Correlation-ID", correlationFor(r)) // inner calls log under the outer request
		// The inner request carries the outer context; drop chi's routing state so the router routes it afresh.
		fresh := http.HandlerFunc(func(w http.ResponseWriter, ir *http.Request) {
			root().ServeHTTP(w, ir.WithContext(context.WithValue(ir.Context(), chi.RouteCtxKey, nil)))
		})
		return &settings.HTTPAPI{Base: "http://hub.internal/api/v1", Header: h, Client: settings.HandlerClient(fresh)}
	}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleAdmin))
			r.Post("/settings/apply", func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Document string `json:"document"`
					Format   string `json:"format"` // informational: yaml or json (both parse as YAML)
					DryRun   bool   `json:"dry_run"`
				}
				if err := decodeJSON(w, r, &in); err != nil {
					fail(w, r, err)
					return
				}
				if in.Document == "" {
					fail(w, r, errBadParam("document is required"))
					return
				}
				res := policy.resolver()
				doc, err := settings.Load(r.Context(), []settings.Source{{Name: "settings", Data: []byte(in.Document)}}, res)
				if err != nil {
					WriteError(w, r, http.StatusUnprocessableEntity, "INVALID_SETTINGS", err.Error(), nil)
					return
				}
				out, err := settings.Apply(r.Context(), inner(r), doc, in.DryRun)
				out.Sources = res.Used()
				if err != nil && !errors.Is(err, settings.ErrAPI) && len(out.Changes) == 0 {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, out) // a partial failure is reported in out.error with what was applied
			})
			r.Get("/settings/export", func(w http.ResponseWriter, r *http.Request) {
				doc, err := settings.Export(r.Context(), inner(r))
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				b, err := settings.Marshal(doc)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, map[string]string{"yaml": string(b)})
			})
			r.Get("/settings/sources", func(w http.ResponseWriter, _ *http.Request) {
				res := policy.resolver()
				enabled := []string{}
				for _, s := range settings.Schemes {
					if res.Allowed[s] {
						enabled = append(enabled, s)
					}
				}
				WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "all": settings.Schemes, "env_prefix": res.EnvPrefix})
			})
		})
	}
}

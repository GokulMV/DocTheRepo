package api

import (
	"encoding/base64"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// SealRoutes mounts GET /seal/key (the public key the UI and CLI seal secrets to) and POST /seal/rotate.
func SealRoutes(a *auth.Service, keys *store.SealKeys) func(chi.Router) {
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/seal/key", func(w http.ResponseWriter, r *http.Request) {
				k, err := keys.Active(r.Context())
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				p := k.Public()
				w.Header().Set("Cache-Control", "private, max-age=300")
				WriteJSON(w, http.StatusOK, map[string]string{"kid": p.ID, "alg": p.Alg,
					"x25519": base64.StdEncoding.EncodeToString(p.X25519), "mlkem768": base64.StdEncoding.EncodeToString(p.MLKEM768)})
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleOwner))
			r.Post("/seal/rotate", func(w http.ResponseWriter, r *http.Request) {
				k, err := keys.Rotate(r.Context())
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				_ = a.Audit(r.Context(), auth.FromContext(r.Context()), "seal.rotate", "seal_key", k.ID, nil, clientIP(r))
				WriteJSON(w, http.StatusOK, map[string]string{"kid": k.ID})
			})
		})
	}
}

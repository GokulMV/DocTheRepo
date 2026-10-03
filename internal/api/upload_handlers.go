package api

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// Upload limits: files per request and bytes per request (each file is also capped by docconv).
const (
	MaxUploadFiles = 20
	MaxUploadBytes = 25 << 20
)

// UploadDeps serve documents people upload to the Library.
type UploadDeps struct {
	Auth   *auth.Service
	Upload func(ctx context.Context, userID, collection string, files []ingest.UploadFile) (ingest.UploadResult, error)
	Store  interface {
		Uploads(ctx context.Context) ([]store.Upload, error)
		GetUpload(ctx context.Context, id string) (store.Upload, error)
		DeleteUpload(ctx context.Context, id string) error
	}
}

// UploadRoutes mounts /library/uploads: anyone signed in reads uploaded documents (they are shared team
// knowledge, like synced Confluence pages); editors add and remove them.
func UploadRoutes(d UploadDeps) func(chi.Router) {
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/library/uploads", func(w http.ResponseWriter, r *http.Request) {
				us, err := d.Store.Uploads(r.Context())
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, map[string]any{"items": us})
			})
			r.Get("/library/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
				u, err := d.Store.GetUpload(r.Context(), chi.URLParam(r, "id"))
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				WriteJSON(w, http.StatusOK, u)
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleEditor))
			r.Post("/library/uploads", func(w http.ResponseWriter, r *http.Request) {
				r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
				if err := r.ParseMultipartForm(8 << 20); err != nil {
					var tooBig *http.MaxBytesError
					if errors.As(err, &tooBig) {
						WriteError(w, r, http.StatusRequestEntityTooLarge, "TOO_LARGE", "one upload is at most 25 MB; send the files in smaller groups", nil)
						return
					}
					WriteError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "send the files as multipart/form-data in the field \"files\"", nil)
					return
				}
				defer r.MultipartForm.RemoveAll()
				heads := r.MultipartForm.File["files"]
				if len(heads) == 0 {
					WriteError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "no files: add them in the field \"files\"", nil)
					return
				}
				if len(heads) > MaxUploadFiles {
					WriteError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "at most 20 files per upload", nil)
					return
				}
				files := make([]ingest.UploadFile, 0, len(heads))
				for _, fh := range heads {
					f, err := fh.Open()
					if err != nil {
						WriteErr(w, r, err)
						return
					}
					b, err := io.ReadAll(f)
					f.Close()
					if err != nil {
						WriteErr(w, r, err)
						return
					}
					files = append(files, ingest.UploadFile{Name: fh.Filename, Data: b})
				}
				p := auth.FromContext(r.Context())
				res, err := d.Upload(r.Context(), p.UserID, r.FormValue("collection"), files)
				if err != nil {
					WriteErr(w, r, err)
					return
				}
				names := make([]string, 0, len(res.Documents))
				for _, doc := range res.Documents {
					names = append(names, doc.Name)
				}
				_ = d.Auth.Audit(r.Context(), p, "library.upload", "library", r.FormValue("collection"), map[string]any{"files": names}, clientIP(r))
				WriteJSON(w, http.StatusCreated, res)
			})
			r.Delete("/library/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				if err := d.Store.DeleteUpload(r.Context(), id); err != nil {
					WriteErr(w, r, err)
					return
				}
				_ = d.Auth.Audit(r.Context(), auth.FromContext(r.Context()), "library.upload_delete", "library", id, nil, clientIP(r))
				w.WriteHeader(http.StatusNoContent)
			})
		})
	}
}

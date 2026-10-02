package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// BrowseRoutes mounts the docs Tree, Palace, and Library (plan § 7.4).
func BrowseRoutes(b *store.Browse, svc *auth.Service) func(chi.Router) {
	h := &browseHandlers{b: b, auth: svc}
	return func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleViewer))
			r.Get("/docs/tree", h.tree)
			r.Get("/docs/node/{id}", h.node)
			r.Get("/palace/entities", h.entities)
			r.Get("/palace/entities/{id}/graph", h.graph)
			r.Get("/palace/overview", h.overview)
			r.Get("/library/shelves", h.shelves)
			r.Get("/library/shelves/{slug}", h.shelf)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireRole(auth.RoleEditor))
			r.Post("/library/shelves", h.createShelf)
			r.Patch("/library/shelves/{id}", h.updateShelf)
			r.Delete("/library/shelves/{id}", h.deleteShelf)
			r.Post("/library/shelves/{id}/items", h.pinItem)
		})
	}
}

type browseHandlers struct {
	b    *store.Browse
	auth *auth.Service
}

// scope resolves the caller's readable repositories as a retrieval scope.
func scopeOf(r *http.Request, svc *auth.Service) (rag.Scope, error) {
	s, err := svc.RepoScope(r.Context(), auth.FromContext(r.Context()))
	if err != nil {
		return rag.Scope{}, err
	}
	return rag.Scope{All: s.All, RepoIDs: s.IDs}, nil
}

func allows(sc rag.Scope, repoID string) bool {
	return sc.All || containsStr(sc.RepoIDs, repoID)
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (h *browseHandlers) tree(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	repoID, parent := r.URL.Query().Get("repo_id"), r.URL.Query().Get("parent_id")
	var nodes []store.TreeNode
	switch {
	case repoID == "":
		nodes, err = h.b.Roots(r.Context(), sc)
	case !allows(sc, repoID):
		WriteErr(w, r, ports.ErrNotFound) // unreadable repos are indistinguishable from missing ones
		return
	default:
		if parent == "" {
			var roots []store.TreeNode
			if roots, err = h.b.Roots(r.Context(), rag.Scope{RepoIDs: []string{repoID}}); err == nil && len(roots) > 0 {
				parent = roots[0].ID
			}
		}
		if err == nil && parent != "" {
			nodes, err = h.b.Children(r.Context(), repoID, parent)
		}
	}
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	if nodes == nil {
		nodes = []store.TreeNode{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (h *browseHandlers) node(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	n, err := h.b.Node(r.Context(), chi.URLParam(r, "id"))
	if err == nil && !allows(sc, n.RepoID) {
		err = ports.ErrNotFound
	}
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, n)
}

func (h *browseHandlers) entities(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	var cur store.EntityCursor
	limit, err := pageParams(r, &cur)
	if err != nil {
		fail(w, r, err)
		return
	}
	q := r.URL.Query()
	es, err := h.b.Entities(r.Context(), q.Get("kind"), strings.TrimSpace(q.Get("q")), q.Get("repo_id"), sc, cur, limit)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, newPage(es, limit, func(e store.Entity) any { return store.EntityCursor{Kind: e.Kind, Key: e.Key} }))
}

func (h *browseHandlers) graph(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	depth := 1
	if v := r.URL.Query().Get("depth"); v != "" {
		if depth, err = strconv.Atoi(v); err != nil || depth < 1 || depth > 3 {
			fail(w, r, errBadParam("depth must be 1, 2 or 3"))
			return
		}
	}
	var kinds []string
	if v := r.URL.Query().Get("edge_kinds"); v != "" {
		kinds = strings.Split(v, ",")
	}
	g, err := h.b.Neighbourhood(r.Context(), chi.URLParam(r, "id"), depth, kinds, sc)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, g)
}

func (h *browseHandlers) overview(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	limit := 300
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > 1000 {
			fail(w, r, errBadParam("limit must be between 1 and 1000"))
			return
		}
	}
	var kinds []string
	for _, k := range strings.Split(r.URL.Query().Get("kinds"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			kinds = append(kinds, k)
		}
	}
	o, err := h.b.Overview(r.Context(), kinds, limit, sc)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, o)
}

func (h *browseHandlers) shelves(w http.ResponseWriter, r *http.Request) {
	ss, err := h.b.Shelves(r.Context())
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"shelves": ss})
}

func (h *browseHandlers) shelf(w http.ResponseWriter, r *http.Request) {
	sc, err := scopeOf(r, h.auth)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	s, err := h.b.Shelf(r.Context(), chi.URLParam(r, "slug"), sc)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, s)
}

func (h *browseHandlers) createShelf(w http.ResponseWriter, r *http.Request) {
	var in library.Shelf
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Slug == "" || in.Title == "" {
		fail(w, r, errBadParam("slug and title are required"))
		return
	}
	id, err := h.b.CreateShelf(r.Context(), in)
	if err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.auth.Audit(r.Context(), auth.FromContext(r.Context()), "library.shelf.create", "shelf", id, map[string]string{"slug": in.Slug}, clientIP(r))
	WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *browseHandlers) updateShelf(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Order       *int    `json:"order"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.b.UpdateShelf(r.Context(), id, in.Title, in.Description, in.Order); err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.auth.Audit(r.Context(), auth.FromContext(r.Context()), "library.shelf.update", "shelf", id, in, clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (h *browseHandlers) deleteShelf(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.b.DeleteShelf(r.Context(), id); err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.auth.Audit(r.Context(), auth.FromContext(r.Context()), "library.shelf.delete", "shelf", id, nil, clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (h *browseHandlers) pinItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemType string `json:"item_type"`
		ItemID   string `json:"item_id"`
		Note     string `json:"note"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if in.ItemID == "" {
		fail(w, r, errBadParam("item_id is required"))
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.b.PinItem(r.Context(), id, in.ItemType, in.ItemID, in.Note); err != nil {
		WriteErr(w, r, err)
		return
	}
	_ = h.auth.Audit(r.Context(), auth.FromContext(r.Context()), "library.item.pin", "shelf", id, in, clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// Package qdrantmock is an in-memory Qdrant REST API: collections, aliases (atomic action lists), point
// upsert/delete, and brute-force cosine search with match-any payload filters.
package qdrantmock

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"

	"github.com/go-chi/chi/v5"
)

type point struct {
	vec     []float64
	payload map[string]any
}

type collection struct {
	dims   int
	points map[string]point
}

// Server is a running mock.
type Server struct {
	*httptest.Server
	mu          sync.Mutex
	collections map[string]*collection
	aliases     map[string]string
	// APIKey, when set, is required in the api-key header.
	APIKey string
}

// New starts an empty mock.
func New() *Server {
	s := &Server{collections: map[string]*collection{}, aliases: map[string]string{}}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.APIKey != "" && r.Header.Get("api-key") != s.APIKey {
				write(w, 401, map[string]any{"status": map[string]any{"error": "unauthorized"}})
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/collections/{name}", s.getCollection)
	r.Put("/collections/{name}", s.createCollection)
	r.Delete("/collections/{name}", s.deleteCollection)
	r.Post("/collections/aliases", s.aliasActions)
	r.Put("/collections/{name}/points", s.upsert)
	r.Post("/collections/{name}/points/delete", s.deletePoints)
	r.Post("/collections/{name}/points/search", s.search)
	r.Post("/collections/{name}/points", s.retrieve)
	s.Server = httptest.NewServer(r)
	return s
}

// Collections lists collection names.
func (s *Server) Collections() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for n := range s.collections {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Alias returns the collection an alias points to.
func (s *Server) Alias(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aliases[name]
}

// Count returns the number of points in a collection.
func (s *Server) Count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.collections[name]; c != nil {
		return len(c.points)
	}
	return 0
}

func (s *Server) resolve(name string) *collection {
	if t, ok := s.aliases[name]; ok {
		name = t
	}
	return s.collections[name]
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.resolve(chi.URLParam(r, "name"))
	if c == nil {
		notFound(w)
		return
	}
	write(w, 200, map[string]any{"result": map[string]any{"status": "green", "points_count": len(c.points),
		"config": map[string]any{"params": map[string]any{"vectors": map[string]any{"size": c.dims, "distance": "Cosine"}}}}})
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Vectors struct {
			Size     int    `json:"size"`
			Distance string `json:"distance"`
		} `json:"vectors"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	name := chi.URLParam(r, "name")
	if _, ok := s.collections[name]; ok {
		write(w, 409, map[string]any{"status": map[string]any{"error": "Collection `" + name + "` already exists!"}})
		return
	}
	if body.Vectors.Size <= 0 || body.Vectors.Distance != "Cosine" {
		write(w, 400, map[string]any{"status": map[string]any{"error": "bad vectors config"}})
		return
	}
	s.collections[name] = &collection{dims: body.Vectors.Size, points: map[string]point{}}
	write(w, 200, map[string]any{"result": true})
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.collections, chi.URLParam(r, "name"))
	write(w, 200, map[string]any{"result": true})
}

func (s *Server) aliasActions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Actions []map[string]map[string]string `json:"actions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]string{}
	for k, v := range s.aliases {
		next[k] = v
	}
	for _, a := range body.Actions {
		if d, ok := a["delete_alias"]; ok {
			delete(next, d["alias_name"])
		}
		if c, ok := a["create_alias"]; ok {
			if _, exists := s.collections[c["collection_name"]]; !exists {
				notFound(w)
				return
			}
			next[c["alias_name"]] = c["collection_name"]
		}
	}
	s.aliases = next // all actions apply atomically
	write(w, 200, map[string]any{"result": true})
}

func (s *Server) upsert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Points []struct {
			ID      string         `json:"id"`
			Vector  []float64      `json:"vector"`
			Payload map[string]any `json:"payload"`
		} `json:"points"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.resolve(chi.URLParam(r, "name"))
	if c == nil {
		notFound(w)
		return
	}
	for _, p := range body.Points {
		if len(p.Vector) != c.dims {
			write(w, 400, map[string]any{"status": map[string]any{"error": "Wrong input: Vector dimension error"}})
			return
		}
	}
	for _, p := range body.Points {
		c.points[p.ID] = point{vec: p.Vector, payload: p.Payload}
	}
	write(w, 200, map[string]any{"result": map[string]any{"status": "completed"}})
}

func (s *Server) deletePoints(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Points []string `json:"points"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.resolve(chi.URLParam(r, "name"))
	if c == nil {
		notFound(w)
		return
	}
	for _, id := range body.Points {
		delete(c.points, id)
	}
	write(w, 200, map[string]any{"result": map[string]any{"status": "completed"}})
}

func (s *Server) retrieve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.resolve(chi.URLParam(r, "name"))
	if c == nil {
		notFound(w)
		return
	}
	out := []map[string]any{}
	for _, id := range body.IDs {
		if p, ok := c.points[id]; ok {
			out = append(out, map[string]any{"id": id, "vector": p.vec, "payload": p.payload})
		}
	}
	write(w, 200, map[string]any{"result": out})
}

type condition struct {
	Key   string `json:"key"`
	Match struct {
		Any []any `json:"any"`
	} `json:"match"`
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Vector []float64 `json:"vector"`
		Limit  int       `json:"limit"`
		Filter *struct {
			Must []condition `json:"must"`
		} `json:"filter"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.resolve(chi.URLParam(r, "name"))
	if c == nil {
		notFound(w)
		return
	}
	if len(body.Vector) != c.dims {
		write(w, 400, map[string]any{"status": map[string]any{"error": "Wrong input: Vector dimension error"}})
		return
	}
	type hit struct {
		id    string
		score float64
		p     map[string]any
	}
	var hits []hit
	for id, p := range c.points {
		if body.Filter != nil && !matches(p.payload, body.Filter.Must) {
			continue
		}
		hits = append(hits, hit{id, cosine(body.Vector, p.vec), p.payload})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].id < hits[j].id
	})
	if body.Limit > 0 && len(hits) > body.Limit {
		hits = hits[:body.Limit]
	}
	out := make([]map[string]any, len(hits))
	for i, h := range hits {
		out[i] = map[string]any{"id": h.id, "score": h.score, "payload": h.p}
	}
	write(w, 200, map[string]any{"result": out})
}

func matches(payload map[string]any, must []condition) bool {
	for _, c := range must {
		v := payload[c.Key]
		ok := false
		for _, a := range c.Match.Any {
			if a == v {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	write(w, 404, map[string]any{"status": map[string]any{"error": "Not found: Collection doesn't exist!"}})
}

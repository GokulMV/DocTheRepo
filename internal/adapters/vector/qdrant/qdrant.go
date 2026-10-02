// Package qdrant implements ports.VectorIndex on Qdrant's REST API. Each index version is a collection
// (<prefix>_v<n>) and reads go through the <prefix> alias, which a reindex swaps atomically (plan § 8.18).
// The embedding lock stays in Postgres (shared with pgvector via vlock), and soft-deleted chunks are
// filtered against the chunks table so a revert can revive vectors without re-embedding.
package qdrant

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/vlock"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Index implements ports.VectorIndex.
type Index struct {
	pool   *pgxpool.Pool
	http   *httpx.Client
	base   string
	apiKey string
	// Prefix names the alias and collections.
	Prefix string
	// Overfetch multiplies k so soft-deleted hits can be dropped and k live ones still returned.
	Overfetch int
}

// New returns a Qdrant index at baseURL (e.g. http://qdrant:6333).
func New(pool *pgxpool.Pool, baseURL, apiKey string, c *httpx.Client) *Index {
	if c == nil {
		c = httpx.New("qdrant")
	}
	return &Index{pool: pool, http: c, base: strings.TrimRight(baseURL, "/"), apiKey: apiKey, Prefix: "dth_chunks", Overfetch: 3}
}

// Kind returns "qdrant".
func (x *Index) Kind() string { return "qdrant" }

func (x *Index) collection(v int) string { return x.Prefix + "_v" + strconv.Itoa(v) }

func (x *Index) call(ctx context.Context, method, path string, in, out any) error {
	h := map[string]string{}
	if x.apiKey != "" {
		h["api-key"] = x.apiKey
	}
	return x.http.JSON(ctx, method, x.base+path, h, in, out)
}

func isNotFound(err error) bool {
	var se *httpx.StatusError
	return errors.As(err, &se) && se.StatusCode == http.StatusNotFound
}

func (x *Index) ensureCollection(ctx context.Context, v, dims int) error {
	err := x.call(ctx, http.MethodGet, "/collections/"+x.collection(v), nil, nil)
	if err == nil {
		return nil
	}
	if !isNotFound(err) {
		return err
	}
	return x.call(ctx, http.MethodPut, "/collections/"+x.collection(v), map[string]any{
		"vectors":     map[string]any{"size": dims, "distance": "Cosine"},
		"hnsw_config": map[string]any{"m": 16, "ef_construct": 64},
	}, nil)
}

func (x *Index) pointAlias(ctx context.Context, v int, replace bool) error {
	var actions []any
	if replace {
		actions = append(actions, map[string]any{"delete_alias": map[string]any{"alias_name": x.Prefix}})
	}
	actions = append(actions, map[string]any{"create_alias": map[string]any{"collection_name": x.collection(v), "alias_name": x.Prefix}})
	return x.call(ctx, http.MethodPost, "/collections/aliases", map[string]any{"actions": actions}, nil)
}

// EnsureIndex creates the first collection, alias, and lock; a different spec returns ErrEmbeddingMismatch.
func (x *Index) EnsureIndex(ctx context.Context, spec ports.EmbeddingSpec) error {
	if err := vlock.Validate(spec); err != nil {
		return err
	}
	return vlock.Locked(ctx, x.pool, func(tx pgx.Tx, st ports.IndexState) error {
		if st.Live != nil {
			if err := vlock.Mismatch(*st.Live, spec); err != nil {
				return err
			}
			return x.ensureCollection(ctx, st.Version, spec.Dimensions)
		}
		if err := x.ensureCollection(ctx, 1, spec.Dimensions); err != nil {
			return fmt.Errorf("create qdrant collection: %w", err)
		}
		if err := x.pointAlias(ctx, 1, false); err != nil {
			return fmt.Errorf("create qdrant alias: %w", err)
		}
		return vlock.Init(ctx, tx, spec)
	})
}

// State reports the live and pending versions.
func (x *Index) State(ctx context.Context) (ports.IndexState, error) { return vlock.State(ctx, x.pool) }

// PointID maps a chunk ID to the UUID Qdrant requires as a point ID.
func PointID(chunkID string) string {
	h := sha256.Sum256([]byte(chunkID))
	b := h[:16]
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Upsert writes vectors to a version (0 = live).
func (x *Index) Upsert(ctx context.Context, version int, vs []ports.ChunkVector) error {
	if len(vs) == 0 {
		return nil
	}
	st, err := x.State(ctx)
	if err != nil {
		return err
	}
	v, dims, err := vlock.Resolve(st, version)
	if err != nil {
		return err
	}
	points := make([]any, 0, len(vs))
	for _, cv := range vs {
		if len(cv.Vector) != dims {
			return ports.Permanent(fmt.Errorf("chunk %s: vector has %d dims, index expects %d", cv.ChunkID, len(cv.Vector), dims))
		}
		payload := map[string]any{"chunk_id": cv.ChunkID, "source": string(cv.Source)}
		if cv.RepoID != "" { // repo-less chunks (Confluence, Jira) have no repo_id, so is_empty can find them
			payload["repo_id"] = cv.RepoID
		}
		points = append(points, map[string]any{"id": PointID(cv.ChunkID), "vector": cv.Vector, "payload": payload})
	}
	if err := x.call(ctx, http.MethodPut, "/collections/"+x.collection(v)+"/points?wait=true", map[string]any{"points": points}, nil); err != nil {
		return fmt.Errorf("qdrant upsert: %w", err)
	}
	return nil
}

// Delete removes points from the live and any pending version.
func (x *Index) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	st, err := x.State(ctx)
	if err != nil || st.Live == nil {
		return err
	}
	pts := make([]string, len(ids))
	for i, id := range ids {
		pts[i] = PointID(id)
	}
	for _, v := range []int{st.Version, st.PendingVersion} {
		if v == 0 {
			continue
		}
		if err := x.call(ctx, http.MethodPost, "/collections/"+x.collection(v)+"/points/delete?wait=true", map[string]any{"points": pts}, nil); err != nil {
			return fmt.Errorf("qdrant delete: %w", err)
		}
	}
	return nil
}

// Rekey copies points to new chunk IDs.
func (x *Index) Rekey(ctx context.Context, oldToNew map[string]string) error {
	if len(oldToNew) == 0 {
		return nil
	}
	st, err := x.State(ctx)
	if err != nil || st.Live == nil {
		return err
	}
	ids := make([]string, 0, len(oldToNew))
	byPoint := map[string]string{}
	for o, n := range oldToNew {
		ids = append(ids, PointID(o))
		byPoint[PointID(o)] = n
	}
	for _, v := range []int{st.Version, st.PendingVersion} {
		if v == 0 {
			continue
		}
		var got struct {
			Result []struct {
				ID      string         `json:"id"`
				Vector  []float32      `json:"vector"`
				Payload map[string]any `json:"payload"`
			} `json:"result"`
		}
		if err := x.call(ctx, http.MethodPost, "/collections/"+x.collection(v)+"/points", map[string]any{"ids": ids, "with_vector": true, "with_payload": true}, &got); err != nil {
			return fmt.Errorf("qdrant fetch points: %w", err)
		}
		var points []any
		for _, p := range got.Result {
			n := byPoint[p.ID]
			if n == "" {
				continue
			}
			p.Payload["chunk_id"] = n
			points = append(points, map[string]any{"id": PointID(n), "vector": p.Vector, "payload": p.Payload})
		}
		if len(points) == 0 {
			continue
		}
		if err := x.call(ctx, http.MethodPut, "/collections/"+x.collection(v)+"/points?wait=true", map[string]any{"points": points}, nil); err != nil {
			return fmt.Errorf("qdrant rekey: %w", err)
		}
	}
	return nil
}

type searchResp struct {
	Result []struct {
		Score   float64        `json:"score"`
		Payload map[string]any `json:"payload"`
	} `json:"result"`
}

// Search returns the k nearest live chunks by cosine similarity.
func (x *Index) Search(ctx context.Context, vec []float32, k int, f ports.VectorFilter) ([]ports.VectorHit, error) {
	if k <= 0 {
		return nil, nil
	}
	var must []any
	if len(f.RepoIDs) > 0 {
		// Repo-scoped readers also see repo-less shared sources (Confluence, Jira).
		shared := make([]string, len(ports.SharedSources))
		for i, s := range ports.SharedSources {
			shared[i] = string(s)
		}
		must = append(must, map[string]any{"should": []any{
			map[string]any{"key": "repo_id", "match": map[string]any{"any": f.RepoIDs}},
			map[string]any{"must": []any{
				map[string]any{"key": "source", "match": map[string]any{"any": shared}},
				map[string]any{"is_empty": map[string]any{"key": "repo_id"}},
			}},
		}})
	}
	if len(f.Sources) > 0 {
		src := make([]string, len(f.Sources))
		for i, s := range f.Sources {
			src[i] = string(s)
		}
		must = append(must, map[string]any{"key": "source", "match": map[string]any{"any": src}})
	}
	req := map[string]any{"vector": vec, "limit": k * max(1, x.Overfetch), "with_payload": true,
		"params": map[string]any{"hnsw_ef": max(64, 2*k)}}
	if len(must) > 0 {
		req["filter"] = map[string]any{"must": must}
	}
	var resp searchResp
	if err := x.call(ctx, http.MethodPost, "/collections/"+x.Prefix+"/points/search", req, &resp); err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}
	hits := make([]ports.VectorHit, 0, len(resp.Result))
	ids := make([]string, 0, len(resp.Result))
	for _, r := range resp.Result {
		id, _ := r.Payload["chunk_id"].(string)
		hits = append(hits, ports.VectorHit{ChunkID: id, Score: r.Score})
		ids = append(ids, id)
	}
	live := map[string]bool{}
	rows, err := x.pool.Query(ctx, "SELECT chunk_id FROM chunks WHERE chunk_id = ANY($1) AND deleted_at IS NULL", ids)
	if err != nil {
		return nil, fmt.Errorf("filter live chunks: %w", err)
	}
	liveIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, id := range liveIDs {
		live[id] = true
	}
	out := hits[:0]
	for _, h := range hits {
		if live[h.ChunkID] && len(out) < k {
			out = append(out, h)
		}
	}
	return out, nil
}

// BeginReindex creates the pending collection for spec (resuming a matching one).
func (x *Index) BeginReindex(ctx context.Context, spec ports.EmbeddingSpec) (int, error) {
	if err := vlock.Validate(spec); err != nil {
		return 0, err
	}
	var version int
	err := vlock.Locked(ctx, x.pool, func(tx pgx.Tx, st ports.IndexState) error {
		v, create, err := vlock.PlanReindex(st, spec)
		version = v
		if err != nil || !create {
			return err
		}
		if err := x.ensureCollection(ctx, v, spec.Dimensions); err != nil {
			return err
		}
		return vlock.SetPending(ctx, tx, v, spec)
	})
	return version, err
}

// SwapReindex re-points the alias in one Qdrant alias transaction, promotes the lock, and drops the old collection.
func (x *Index) SwapReindex(ctx context.Context, version int) error {
	return vlock.Locked(ctx, x.pool, func(tx pgx.Tx, st ports.IndexState) error {
		if st.Pending == nil || st.PendingVersion != version {
			return ports.Permanent(fmt.Errorf("version %d is not the pending reindex", version))
		}
		if err := x.pointAlias(ctx, version, true); err != nil {
			return err
		}
		if err := vlock.Promote(ctx, tx); err != nil {
			return err
		}
		if err := x.call(ctx, http.MethodDelete, "/collections/"+x.collection(st.Version), nil, nil); err != nil && !isNotFound(err) {
			return err
		}
		return nil
	})
}

// AbortReindex drops the pending collection.
func (x *Index) AbortReindex(ctx context.Context, version int) error {
	return vlock.Locked(ctx, x.pool, func(tx pgx.Tx, st ports.IndexState) error {
		if st.Pending == nil || st.PendingVersion != version {
			return nil
		}
		if err := x.call(ctx, http.MethodDelete, "/collections/"+x.collection(version), nil, nil); err != nil && !isNotFound(err) {
			return err
		}
		return vlock.ClearPending(ctx, tx)
	})
}

var _ ports.VectorIndex = (*Index)(nil)

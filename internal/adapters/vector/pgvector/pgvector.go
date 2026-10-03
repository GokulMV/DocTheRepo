// Package pgvector implements ports.VectorIndex on PostgreSQL + pgvector. Embeddings live in versioned
// tables (chunk_embeddings_v<n>) sized for the locked model; reads go through the chunk_embeddings view,
// which a reindex re-points in the same transaction that drops the old table (plan § 8.18).
package pgvector

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/vlock"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

const viewName = "chunk_embeddings"

// Index implements ports.VectorIndex.
type Index struct {
	pool *pgxpool.Pool
	// EfSearch is the HNSW candidate list size; raised to 2k when a search asks for more.
	EfSearch int
}

// New returns the pgvector index.
func New(pool *pgxpool.Pool) *Index { return &Index{pool: pool, EfSearch: 64} }

// Kind returns "pgvector".
func (x *Index) Kind() string { return "pgvector" }

func table(version int) string { return "chunk_embeddings_v" + strconv.Itoa(version) }

func createTable(ctx context.Context, tx pgx.Tx, version, dims int) error {
	t := table(version)
	_, err := tx.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
		chunk_id  text PRIMARY KEY REFERENCES chunks(chunk_id) ON DELETE CASCADE,
		repo_id   uuid,
		source    chunk_source NOT NULL,
		embedding vector(%[2]d) NOT NULL
	);
	CREATE INDEX IF NOT EXISTS %[1]s_hnsw ON %[1]s USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
	CREATE INDEX IF NOT EXISTS %[1]s_repo ON %[1]s (repo_id)`, t, dims))
	return err
}

func pointView(ctx context.Context, tx pgx.Tx, version int) error {
	_, err := tx.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s; CREATE VIEW %s AS SELECT chunk_id, repo_id, source, embedding FROM %s",
		viewName, viewName, table(version)))
	return err
}

// EnsureIndex creates the extension, the first version table, and the lock; a different spec than the
// locked one returns ports.ErrEmbeddingMismatch.
func (x *Index) EnsureIndex(ctx context.Context, spec ports.EmbeddingSpec) error {
	if err := vlock.Validate(spec); err != nil {
		return err
	}
	return vlock.Locked(ctx, x.pool, func(tx pgx.Tx, st ports.IndexState) error {
		if st.Live != nil {
			return vlock.Mismatch(*st.Live, spec)
		}
		if _, err := tx.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
			return fmt.Errorf("enable pgvector: %w", err)
		}
		if err := vlock.Init(ctx, tx, spec); err != nil {
			return err
		}
		if err := createTable(ctx, tx, 1, spec.Dimensions); err != nil {
			return err
		}
		return pointView(ctx, tx, 1)
	})
}

// State reports the live and pending versions.
func (x *Index) State(ctx context.Context) (ports.IndexState, error) { return vlock.State(ctx, x.pool) }

func (x *Index) versionDims(ctx context.Context, version int) (int, int, error) {
	st, err := x.State(ctx)
	if err != nil {
		return 0, 0, err
	}
	return vlock.Resolve(st, version)
}

// Upsert writes vectors to a version (0 = live).
func (x *Index) Upsert(ctx context.Context, version int, vs []ports.ChunkVector) error {
	if len(vs) == 0 {
		return nil
	}
	v, dims, err := x.versionDims(ctx, version)
	if err != nil {
		return err
	}
	b := &pgx.Batch{}
	q := fmt.Sprintf(`INSERT INTO %s (chunk_id, repo_id, source, embedding) VALUES ($1, $2, $3, $4::vector)
		ON CONFLICT (chunk_id) DO UPDATE SET repo_id = EXCLUDED.repo_id, source = EXCLUDED.source, embedding = EXCLUDED.embedding`, table(v))
	for _, cv := range vs {
		if len(cv.Vector) != dims {
			return ports.Permanent(fmt.Errorf("chunk %s: vector has %d dims, index expects %d", cv.ChunkID, len(cv.Vector), dims))
		}
		var repo *string
		if cv.RepoID != "" {
			repo = &cv.RepoID
		}
		b.Queue(q, cv.ChunkID, repo, string(cv.Source), Encode(cv.Vector))
	}
	if err := x.pool.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("upsert embeddings: %w", err)
	}
	return nil
}

// Delete removes vectors from the live and any pending version.
func (x *Index) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	st, err := x.State(ctx)
	if err != nil || st.Live == nil {
		return err
	}
	for _, v := range []int{st.Version, st.PendingVersion} {
		if v == 0 {
			continue
		}
		if _, err := x.pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE chunk_id = ANY($1)", table(v)), ids); err != nil {
			return fmt.Errorf("delete embeddings: %w", err)
		}
	}
	return nil
}

// Rekey copies vectors to new chunk IDs.
func (x *Index) Rekey(ctx context.Context, oldToNew map[string]string) error {
	if len(oldToNew) == 0 {
		return nil
	}
	st, err := x.State(ctx)
	if err != nil || st.Live == nil {
		return err
	}
	olds, news := make([]string, 0, len(oldToNew)), make([]string, 0, len(oldToNew))
	for o, n := range oldToNew {
		olds, news = append(olds, o), append(news, n)
	}
	for _, v := range []int{st.Version, st.PendingVersion} {
		if v == 0 {
			continue
		}
		if _, err := x.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %[1]s (chunk_id, repo_id, source, embedding)
			SELECT m.new_id, e.repo_id, e.source, e.embedding FROM unnest($1::text[], $2::text[]) AS m(old_id, new_id)
			JOIN %[1]s e ON e.chunk_id = m.old_id
			ON CONFLICT (chunk_id) DO UPDATE SET repo_id = EXCLUDED.repo_id, source = EXCLUDED.source, embedding = EXCLUDED.embedding`, table(v)),
			olds, news); err != nil {
			return fmt.Errorf("rekey embeddings: %w", err)
		}
	}
	return nil
}

// Search returns the k nearest live chunks by cosine similarity.
func (x *Index) Search(ctx context.Context, vec []float32, k int, f ports.VectorFilter) ([]ports.VectorHit, error) {
	if k <= 0 {
		return nil, nil
	}
	var repos, sources []string
	repos = f.RepoIDs
	for _, s := range f.Sources {
		sources = append(sources, string(s))
	}
	var hits []ports.VectorHit
	err := pgx.BeginFunc(ctx, x.pool, func(tx pgx.Tx) error {
		ef := max(x.EfSearch, 2*k)
		if _, err := tx.Exec(ctx, "SELECT set_config('hnsw.ef_search', $1, true)", strconv.Itoa(ef)); err != nil {
			return err
		}
		// pgvector ≥ 0.8: keep scanning the graph when filters reject candidates, so filtered searches still
		// return k rows. Older versions ignore the unknown setting only when it is custom-prefixed, so probe.
		_, _ = tx.Exec(ctx, "SAVEPOINT it")
		if _, err := tx.Exec(ctx, "SET LOCAL hnsw.iterative_scan = relaxed_order"); err != nil {
			_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT it")
		}
		rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT e.chunk_id, 1 - (e.embedding <=> $1::vector) AS score
			FROM %s e JOIN chunks c ON c.chunk_id = e.chunk_id AND c.deleted_at IS NULL
			WHERE (cardinality($2::uuid[]) = 0 OR e.repo_id = ANY($2::uuid[]) OR (e.repo_id IS NULL AND e.source::text IN ('confluence', 'jira', 'notion', 'upload')))
			  AND (cardinality($3::text[]) = 0 OR e.source::text = ANY($3::text[]))
			ORDER BY e.embedding <=> $1::vector LIMIT $4`, viewName), Encode(vec), nonNil(repos), nonNil(sources), k)
		if err != nil {
			return err
		}
		hits, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ports.VectorHit, error) {
			var h ports.VectorHit
			err := r.Scan(&h.ChunkID, &h.Score)
			return h, err
		})
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // undefined_table: nothing has been embedded yet
		return nil, fmt.Errorf("vector search: index not initialised: %w", ports.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	return hits, nil
}

// BeginReindex creates the pending version for spec (resuming it when the same spec is already pending).
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
		if err := createTable(ctx, tx, v, spec.Dimensions); err != nil {
			return err
		}
		return vlock.SetPending(ctx, tx, v, spec)
	})
	return version, err
}

// SwapReindex makes the pending version live, re-points the view, and drops the old table in one transaction.
func (x *Index) SwapReindex(ctx context.Context, version int) error {
	return vlock.Locked(ctx, x.pool, func(tx pgx.Tx, st ports.IndexState) error {
		if st.Pending == nil || st.PendingVersion != version {
			return ports.Permanent(fmt.Errorf("version %d is not the pending reindex", version))
		}
		if err := pointView(ctx, tx, version); err != nil {
			return err
		}
		if err := vlock.Promote(ctx, tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+table(st.Version))
		return err
	})
}

// AbortReindex drops the pending version.
func (x *Index) AbortReindex(ctx context.Context, version int) error {
	return vlock.Locked(ctx, x.pool, func(tx pgx.Tx, st ports.IndexState) error {
		if st.Pending == nil || st.PendingVersion != version {
			return nil
		}
		if _, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+table(version)); err != nil {
			return err
		}
		return vlock.ClearPending(ctx, tx)
	})
}

// Encode renders a vector in pgvector's text format.
func Encode(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

var _ ports.VectorIndex = (*Index)(nil)

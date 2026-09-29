// Package vlock manages the embedding_lock row every vector index adapter shares: the live embedding
// space and version, and the pending version of a running reindex (plan § 8.18). All changes happen under
// a transaction-scoped advisory lock so replicas never race on index DDL.
package vlock

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// IndexName is the lock row for the chunk index.
const IndexName = "chunks"

const lockKey = 7_301_022_001

// Read returns the index state; Live is nil before the first EnsureIndex.
func Read(ctx context.Context, q pgx.Tx) (ports.IndexState, error) {
	var (
		st          ports.IndexState
		kind, model string
		dims, ver   int
		pKind       *string
		pModel      *string
		pDims, pVer *int32
	)
	err := q.QueryRow(ctx, `SELECT provider_kind::text, model, dimensions, version, pending_provider_kind::text, pending_model,
		pending_dimensions, pending_version FROM embedding_lock WHERE index_name = $1`, IndexName).
		Scan(&kind, &model, &dims, &ver, &pKind, &pModel, &pDims, &pVer)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Live, st.Version = &ports.EmbeddingSpec{ProviderKind: kind, Model: model, Dimensions: dims}, ver
	if pVer != nil && pKind != nil && pModel != nil && pDims != nil {
		st.PendingVersion = int(*pVer)
		st.Pending = &ports.EmbeddingSpec{ProviderKind: *pKind, Model: *pModel, Dimensions: int(*pDims)}
	}
	return st, nil
}

// State reads the index state outside a lock.
func State(ctx context.Context, pool *pgxpool.Pool) (ports.IndexState, error) {
	var st ports.IndexState
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		st, err = Read(ctx, tx)
		return err
	})
	return st, err
}

// Locked runs fn in a transaction holding the index advisory lock, with the current state.
func Locked(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx, st ports.IndexState) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return err
	}
	st, err := Read(ctx, tx)
	if err != nil {
		return err
	}
	if err := fn(tx, st); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Validate checks a spec.
func Validate(spec ports.EmbeddingSpec) error {
	if spec.Dimensions <= 0 || spec.Model == "" {
		return errors.New("embedding spec needs a model and positive dimensions")
	}
	return nil
}

// Mismatch returns ErrEmbeddingMismatch when spec differs from the live one.
func Mismatch(live, spec ports.EmbeddingSpec) error {
	if live != spec {
		return fmt.Errorf("index holds %s/%s (%d dims), configured %s/%s (%d dims): %w",
			live.ProviderKind, live.Model, live.Dimensions, spec.ProviderKind, spec.Model, spec.Dimensions, ports.ErrEmbeddingMismatch)
	}
	return nil
}

// Init inserts the lock at version 1.
func Init(ctx context.Context, tx pgx.Tx, spec ports.EmbeddingSpec) error {
	_, err := tx.Exec(ctx, `INSERT INTO embedding_lock (index_name, provider_kind, model, dimensions, version) VALUES ($1, $2, $3, $4, 1)`,
		IndexName, spec.ProviderKind, spec.Model, spec.Dimensions)
	if err != nil {
		return fmt.Errorf("lock embedding model: %w", err)
	}
	return nil
}

// PlanReindex decides the pending version for spec: the running one when it matches (resume), otherwise
// live+1 (create=true). A different running reindex is a permanent error.
func PlanReindex(st ports.IndexState, spec ports.EmbeddingSpec) (version int, create bool, err error) {
	if st.Live == nil {
		return 0, false, fmt.Errorf("vector index is not initialised: %w", ports.ErrNotFound)
	}
	if st.Pending != nil {
		if *st.Pending == spec {
			return st.PendingVersion, false, nil
		}
		return 0, false, ports.Permanent(fmt.Errorf("a reindex to %s/%s is already running; abort it first", st.Pending.ProviderKind, st.Pending.Model))
	}
	return st.Version + 1, true, nil
}

// SetPending records the pending version.
func SetPending(ctx context.Context, tx pgx.Tx, version int, spec ports.EmbeddingSpec) error {
	_, err := tx.Exec(ctx, `UPDATE embedding_lock SET pending_version = $2, pending_provider_kind = $3, pending_model = $4,
		pending_dimensions = $5 WHERE index_name = $1`, IndexName, version, spec.ProviderKind, spec.Model, spec.Dimensions)
	return err
}

// Promote makes the pending version live.
func Promote(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE embedding_lock SET version = pending_version, provider_kind = pending_provider_kind,
		model = pending_model, dimensions = pending_dimensions, pending_version = NULL, pending_provider_kind = NULL,
		pending_model = NULL, pending_dimensions = NULL WHERE index_name = $1`, IndexName)
	return err
}

// ClearPending forgets the pending version.
func ClearPending(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE embedding_lock SET pending_version = NULL, pending_provider_kind = NULL, pending_model = NULL,
		pending_dimensions = NULL WHERE index_name = $1`, IndexName)
	return err
}

// Resolve maps a requested version (0 = live) to (version, dims).
func Resolve(st ports.IndexState, version int) (int, int, error) {
	if st.Live == nil {
		return 0, 0, fmt.Errorf("vector index is not initialised: %w", ports.ErrNotFound)
	}
	switch {
	case version == 0 || version == st.Version:
		return st.Version, st.Live.Dimensions, nil
	case st.Pending != nil && version == st.PendingVersion:
		return version, st.Pending.Dimensions, nil
	}
	return 0, 0, fmt.Errorf("index version %d does not exist: %w", version, ports.ErrNotFound)
}

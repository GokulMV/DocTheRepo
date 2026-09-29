// Package store owns PostgreSQL: the connection pool, migrations, and the repositories the core uses.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GokulMV/DocTheRepo/internal/store/gen"
	"github.com/GokulMV/DocTheRepo/internal/store/schema"
)

// Store bundles the pool and the generated queries.
type Store struct {
	Pool *pgxpool.Pool
	Q    *gen.Queries
}

// Options configures Open.
type Options struct {
	URL              string
	MaxConns         int32
	ConnectTimeout   time.Duration
	StatementTimeout time.Duration
}

// Open connects to Postgres and verifies the connection.
func Open(ctx context.Context, o Options) (*Store, error) {
	if o.URL == "" {
		return nil, errors.New("database URL is empty: set the env var named by database.url_env (default DTH_DATABASE_URL)")
	}
	pc, err := pgxpool.ParseConfig(o.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	if o.MaxConns > 0 {
		pc.MaxConns = o.MaxConns
	}
	if o.StatementTimeout > 0 {
		pc.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", o.StatementTimeout.Milliseconds())
	}
	pc.ConnConfig.RuntimeParams["application_name"] = "dth-hub"
	cctx := ctx
	if o.ConnectTimeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, o.ConnectTimeout)
		defer cancel()
	}
	pool, err := pgxpool.NewWithConfig(cctx, pc)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(cctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{Pool: pool, Q: gen.New(pool)}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.Pool.Close() }

// Ping checks connectivity; used by /readyz.
func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }

// Migrate applies all pending up-migrations (see schema.Up).
func (s *Store) Migrate(ctx context.Context) (uint, error) { return schema.Up(s.Pool) }

// MigrationVersion reports the applied version without changing anything.
func (s *Store) MigrationVersion(ctx context.Context) (uint, error) { return schema.Version(s.Pool) }

// ForceMigration marks version as applied and clean.
func (s *Store) ForceMigration(ctx context.Context, version int) (uint, error) {
	return schema.Force(s.Pool, version)
}

// InTx runs fn in a transaction, committing on success and rolling back on error or panic.
func (s *Store) InTx(ctx context.Context, fn func(q *gen.Queries, tx pgx.Tx) error) (err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(s.Q.WithTx(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsNoRows reports whether err is pgx's "no rows" error.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// strPtr maps "" to NULL for nullable text/uuid parameters.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

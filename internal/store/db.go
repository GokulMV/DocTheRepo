// Package store owns PostgreSQL: the connection pool, migrations, and the repositories the core uses.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/GokulMV/DocTheRepo/internal/store/gen"
	"github.com/GokulMV/DocTheRepo/migrations"
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

// Migrate applies all pending up-migrations. golang-migrate takes a Postgres advisory lock, so several
// replicas starting at once apply each migration exactly once.
func (s *Store) Migrate(ctx context.Context) (uint, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("load embedded migrations: %w", err)
	}
	db := stdlib.OpenDBFromPool(s.Pool)
	drv, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		_ = db.Close()
		return 0, fmt.Errorf("init migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		_ = drv.Close()
		return 0, fmt.Errorf("init migrator: %w", err)
	}
	// The driver holds a dedicated connection for its advisory lock; closing the migrator returns it to the
	// pool (otherwise Pool.Close would wait on it forever).
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	v, dirty, err := m.Version()
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	if dirty {
		return v, fmt.Errorf("migration %d is dirty: fix the failed migration, then run `dth migrate force %d`", v, v)
	}
	return v, nil
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

// Package schema applies the embedded migrations. It is a leaf (no core imports, no CGO) so the dth CLI
// can migrate a database without linking the Tree-sitter grammars the rest of the store depends on.
package schema

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/GokulMV/DocTheRepo/migrations"
)

// Up applies all pending up-migrations. golang-migrate takes a Postgres advisory lock, so several
// replicas starting at once apply each migration exactly once.
func Up(pool *pgxpool.Pool) (uint, error) {
	return withMigrator(pool, func(m *migrate.Migrate) error {
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("apply migrations: %w", err)
		}
		return nil
	})
}

// Version reports the applied version without changing anything.
func Version(pool *pgxpool.Pool) (uint, error) {
	return withMigrator(pool, func(*migrate.Migrate) error { return nil })
}

// Force marks version as applied and clean (recovery after a failed migration was fixed by hand).
func Force(pool *pgxpool.Pool, version int) (uint, error) {
	return withMigrator(pool, func(m *migrate.Migrate) error { return m.Force(version) })
}

func withMigrator(pool *pgxpool.Pool, fn func(m *migrate.Migrate) error) (uint, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("load embedded migrations: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
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
	if err := fn(m); err != nil {
		return 0, err
	}
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	if dirty {
		return v, fmt.Errorf("migration %d is dirty: fix the failed migration, then run `dth migrate force %d`", v, v)
	}
	return v, nil
}

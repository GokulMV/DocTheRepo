// Package storetest provides a real PostgreSQL + pgvector instance for integration tests: one container
// per test binary, a migrated template database, and a fresh database cloned from it for every test.
package storetest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// Image is the Postgres image used for tests; it must match what deployments run.
const Image = "pgvector/pgvector:pg16"

const templateDB = "dth_template"

var (
	once     sync.Once
	baseURL  string // admin connection to the "postgres" database
	setupErr error
)

// setup starts the container (or uses DTH_TEST_DATABASE_URL) and builds the migrated template once.
func setup() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if u := os.Getenv("DTH_TEST_DATABASE_URL"); u != "" {
		baseURL = u
	} else {
		c, err := tcpostgres.Run(ctx, Image,
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(2*time.Minute)),
		)
		if err != nil {
			setupErr = fmt.Errorf("start postgres container: %w", err)
			return
		}
		baseURL, setupErr = c.ConnectionString(ctx, "sslmode=disable")
		if setupErr != nil {
			return
		}
	}
	setupErr = exec(ctx, baseURL, "DROP DATABASE IF EXISTS "+templateDB, "CREATE DATABASE "+templateDB)
	if setupErr != nil {
		return
	}
	st, err := store.Open(ctx, store.Options{URL: withDB(baseURL, templateDB), MaxConns: 4})
	if err != nil {
		setupErr = err
		return
	}
	defer st.Close()
	if _, err := st.Migrate(ctx); err != nil {
		setupErr = fmt.Errorf("migrate template: %w", err)
	}
}

// New returns a Store on a fresh, fully migrated database dedicated to this test. It skips the test when
// no container runtime is available and DTH_REQUIRE_DOCKER is unset, so unit-only environments still run.
func New(t testing.TB) *store.Store {
	t.Helper()
	once.Do(setup)
	if setupErr != nil {
		if os.Getenv("DTH_REQUIRE_DOCKER") != "" {
			t.Fatalf("integration database unavailable: %v", setupErr)
		}
		t.Skipf("integration database unavailable (set DTH_REQUIRE_DOCKER=1 to fail instead): %v", setupErr)
	}
	ctx := context.Background()
	name := "t_" + strings.ReplaceAll(ports.NewID(), "-", "")
	if err := exec(ctx, baseURL, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDB)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	st, err := store.Open(ctx, store.Options{URL: withDB(baseURL, name), MaxConns: 20})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		st.Close()
		_ = exec(context.Background(), baseURL, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name))
	})
	return st
}

// URL returns a connection string for a fresh migrated database without opening a Store.
func URL(t testing.TB) string {
	t.Helper()
	st := New(t)
	return st.Pool.Config().ConnString()
}

func exec(ctx context.Context, dsn string, stmts ...string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

func withDB(dsn, db string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + db
	return u.String()
}

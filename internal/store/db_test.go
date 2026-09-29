package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/migrations"
)

func TestMigrate_IsIdempotent(t *testing.T) {
	st := storetest.New(t)
	v, err := st.Migrate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, latestMigration(t), v)
}

// latestMigration reads the highest NNNN prefix from the embedded migration files.
func latestMigration(t *testing.T) uint {
	entries, err := migrations.FS.ReadDir(".")
	require.NoError(t, err)
	var max uint
	for _, e := range entries {
		var n uint
		if _, err := fmt.Sscanf(e.Name(), "%04d_", &n); err == nil && n > max {
			max = n
		}
	}
	return max
}

func TestMigrations_DownThenUp_RoundTrips(t *testing.T) {
	st := storetest.New(t)
	src, err := iofs.New(migrations.FS, ".")
	require.NoError(t, err)
	drv, err := migratepgx.WithInstance(stdlib.OpenDBFromPool(st.Pool), &migratepgx.Config{})
	require.NoError(t, err)
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	require.NoError(t, err)
	defer m.Close()
	require.NoError(t, m.Down(), "every down migration must apply cleanly")
	require.NoError(t, m.Up(), "and the schema must come back")
}

func TestUsagePartitions_CreatedForCurrentMonths(t *testing.T) {
	st := storetest.New(t)
	var n int
	err := st.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_inherits i JOIN pg_class p ON p.oid = i.inhparent WHERE p.relname = 'usage_events'`).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 4, n, "current month plus three ahead")
	_, err = st.Pool.Exec(context.Background(), "SELECT ensure_month_partitions('usage_events', 3)")
	require.NoError(t, err, "re-running partition maintenance is a no-op")
}

func TestInTx_RollsBackOnError(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	err := st.InTx(ctx, func(q *gen.Queries, _ pgx.Tx) error {
		_, err := q.EnqueueJob(ctx, gen.EnqueueJobParams{ID: "01920000-0000-7000-8000-000000000001", Type: "code_push",
			Payload: []byte("{}"), MaxAttempts: 5, CorrelationID: "c"})
		require.NoError(t, err)
		return assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)
	_, err = st.Q.GetJob(ctx, "01920000-0000-7000-8000-000000000001")
	assert.True(t, store.IsNoRows(err), "the insert was rolled back")
}

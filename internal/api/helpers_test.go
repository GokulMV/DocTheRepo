package api_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// seedRepo creates a GitHub connector and a repo; it returns both IDs.
func seedRepo(t *testing.T, st *store.Store, name string) (string, string) {
	t.Helper()
	cid := ports.NewID()
	_, err := st.Pool.Exec(context.Background(), `INSERT INTO connectors (id, type, name) VALUES ($1, 'github', $2)`, cid, "gh-"+cid[:8])
	require.NoError(t, err)
	return seedRepoOn(t, st, cid, name)
}

func seedRepoOn(t *testing.T, st *store.Store, connectorID, name string) (string, string) {
	t.Helper()
	id, err := store.NewRepos(st).Upsert(context.Background(), ports.RepoConfig{ConnectorID: connectorID, FullName: name, DefaultBranch: "main"})
	require.NoError(t, err)
	return connectorID, id
}

package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// The agent reads files and lists paths only inside the asker's repositories.
func TestAgentExplorerIsScoped(t *testing.T) {
	st, _, cid, shopID := fixture(t)
	ctx := context.Background()
	secretID, err := store.NewRepos(st).Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: "acme/secret", DefaultBranch: "main"})
	require.NoError(t, err)
	mk := func(repo, scope, id, path string) ports.Chunk {
		return ports.Chunk{ID: id, RepoID: repo, Scope: scope, Source: ports.SourceCode, Path: path, Symbol: id, Language: "go", Content: id, ContentHash: id}
	}
	_, err = store.NewChunks(st).Apply(ctx, store.ChunkWrite{Upserts: []ports.Chunk{
		mk(shopID, "acme/shop", "r1", "internal/keys/rotation.go"), mk(shopID, "acme/shop", "r2", "internal/keys/rotation.go"),
		mk(shopID, "acme/shop", "o1", "internal/keys/other.go"), mk(secretID, "acme/secret", "s1", "internal/keys/rotation.go"),
	}})
	require.NoError(t, err)
	qa := store.NewQA(st)
	shopOnly := rag.Scope{RepoIDs: []string{shopID}}

	cs, err := qa.ChunksForPath(ctx, "keys/rotation.go", shopOnly, 10)
	require.NoError(t, err)
	ids := []string{}
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	assert.ElementsMatch(t, []string{"r1", "r2"}, ids, "matched by path suffix, never another repository's file")
	cs, _ = qa.ChunksForPath(ctx, "rotation.go", rag.Scope{All: true}, 10)
	assert.Len(t, cs, 3)

	ps, err := qa.ListPaths(ctx, "keys/", shopOnly, 10)
	require.NoError(t, err)
	assert.Equal(t, []rag.PathEntry{{Repo: "acme/shop", Path: "internal/keys/other.go", Chunks: 1}, {Repo: "acme/shop", Path: "internal/keys/rotation.go", Chunks: 2}}, ps)
}

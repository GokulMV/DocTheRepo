package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// A cached answer outlives writes elsewhere and expires when what it cites changes.
func TestAnswerCacheFreshness(t *testing.T) {
	st, _, cid, shopID := fixture(t)
	ctx := context.Background()
	otherID, err := store.NewRepos(st).Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: "acme/other", DefaultBranch: "main"})
	require.NoError(t, err)
	cs, qa := store.NewChunks(st), store.NewQA(st)
	mk := func(repoID, scope, id, content string) ports.Chunk {
		return ports.Chunk{ID: id, RepoID: repoID, Scope: scope, Source: ports.SourceCode, Path: id + ".go", Symbol: id, Language: "go",
			Content: content, ContentHash: content}
	}
	write := func(w store.ChunkWrite) {
		time.Sleep(5 * time.Millisecond) // updated_at must be after the answer's created_at
		_, err := cs.Apply(ctx, w)
		require.NoError(t, err)
	}
	write(store.ChunkWrite{Upserts: []ports.Chunk{mk(shopID, "acme/shop", "pay", "1"), mk(shopID, "acme/shop", "cart", "1"), mk(otherID, "acme/other", "x", "1")}})
	put := func() {
		require.NoError(t, qa.PutAnswer(ctx, "k", rag.Answer{Text: "Payments retry [1].", Citations: []rag.Citation{{N: 1, ChunkID: "pay"}}}))
	}
	hit := func() bool {
		_, ok, err := qa.CachedAnswer(ctx, "k")
		require.NoError(t, err)
		return ok
	}

	put()
	require.True(t, hit())
	write(store.ChunkWrite{Upserts: []ports.Chunk{mk(otherID, "acme/other", "x", "2")}})
	assert.True(t, hit(), "a commit to another repository keeps the answer")

	write(store.ChunkWrite{Upserts: []ports.Chunk{mk(shopID, "acme/shop", "cart", "2")}})
	assert.False(t, hit(), "a change in the cited repository expires it")

	put()
	require.True(t, hit())
	write(store.ChunkWrite{Drop: []string{"pay"}})
	assert.False(t, hit(), "a cited chunk that is gone expires it")
}

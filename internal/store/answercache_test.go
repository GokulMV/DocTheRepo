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

// A reworded question finds the cached answer by meaning, within its scope and while its sources hold.
func TestSimilarAnswer(t *testing.T) {
	st, _, _, shopID := fixture(t)
	ctx := context.Background()
	cs, qa := store.NewChunks(st), store.NewQA(st)
	_, err := cs.Apply(ctx, store.ChunkWrite{Upserts: []ports.Chunk{{ID: "pay", RepoID: shopID, Scope: "acme/shop", Source: ports.SourceCode,
		Path: "pay.go", Symbol: "pay", Language: "go", Content: "1", ContentHash: "1"}}})
	require.NoError(t, err)
	shop := rag.ScopeKey(rag.Scope{RepoIDs: []string{shopID}})
	vec := []float32{1, 0, 0.2}
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, qa.PutAnswerMeaning(ctx, "k1", "How do payment retries work?", shop, "embed-1", vec,
		rag.Answer{Text: "Payments retry [1].", Citations: []rag.Citation{{N: 1, ChunkID: "pay"}}}))
	all := func(string) bool { return true }

	a, ok, err := qa.SimilarAnswer(ctx, shop, "embed-1", []float32{0.98, 0.02, 0.21}, 0.95, all)
	require.NoError(t, err)
	require.True(t, ok, "a close rewording hits")
	assert.Equal(t, "Payments retry [1].", a.Text)
	require.Len(t, a.Citations, 1)

	_, ok, _ = qa.SimilarAnswer(ctx, shop, "embed-1", []float32{0, 1, 0}, 0.95, all)
	assert.False(t, ok, "a different meaning misses")
	_, ok, _ = qa.SimilarAnswer(ctx, rag.ScopeKey(rag.Scope{All: true}), "embed-1", vec, 0.95, all)
	assert.False(t, ok, "another scope misses")
	_, ok, _ = qa.SimilarAnswer(ctx, shop, "embed-2", vec, 0.95, all)
	assert.False(t, ok, "another embedding model misses")
	var asked string
	_, ok, _ = qa.SimilarAnswer(ctx, shop, "embed-1", vec, 0.95, func(q string) bool { asked = q; return false })
	assert.False(t, ok, "the caller can refuse a candidate")
	assert.Equal(t, "How do payment retries work?", asked)

	time.Sleep(5 * time.Millisecond)
	_, err = cs.Apply(ctx, store.ChunkWrite{Upserts: []ports.Chunk{{ID: "pay", RepoID: shopID, Scope: "acme/shop", Source: ports.SourceCode,
		Path: "pay.go", Symbol: "pay", Language: "go", Content: "2", ContentHash: "2"}}})
	require.NoError(t, err)
	_, ok, _ = qa.SimilarAnswer(ctx, shop, "embed-1", vec, 0.95, all)
	assert.False(t, ok, "a change in what it cites expires it")
}

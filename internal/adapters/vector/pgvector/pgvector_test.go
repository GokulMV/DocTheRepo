package pgvector

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

var spec3 = ports.EmbeddingSpec{ProviderKind: "openai", Model: "e3", Dimensions: 3}

func seed(t *testing.T, st *store.Store, ids ...string) {
	t.Helper()
	var cs []ports.Chunk
	for _, id := range ids {
		src := ports.SourceCode
		if id[0] == 'd' {
			src = ports.SourceGeneratedDoc
		}
		cs = append(cs, ports.Chunk{ID: id, Scope: "s", Source: src, Path: "p", Symbol: id, Content: id, ContentHash: id})
	}
	_, err := store.NewChunks(st).Apply(context.Background(), store.ChunkWrite{Upserts: cs})
	require.NoError(t, err)
}

func TestEnsureIndexLocksModel(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	x := New(st.Pool)
	assert.Equal(t, "pgvector", x.Kind())
	_, _, err := x.versionDims(ctx, 0)
	assert.ErrorIs(t, err, ports.ErrNotFound)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = x.EnsureIndex(ctx, spec3) }()
	}
	wg.Wait()
	for _, e := range errs {
		require.NoError(t, e, "concurrent replicas initialise once")
	}
	require.NoError(t, x.EnsureIndex(ctx, spec3))
	assert.ErrorIs(t, x.EnsureIndex(ctx, ports.EmbeddingSpec{ProviderKind: "openai", Model: "e4", Dimensions: 4}), ports.ErrEmbeddingMismatch)
	assert.Error(t, x.EnsureIndex(ctx, ports.EmbeddingSpec{}))
	s, err := x.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, s.Version)
	assert.Equal(t, &spec3, s.Live)
}

func TestUpsertSearchDelete(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	x := New(st.Pool)
	require.NoError(t, x.EnsureIndex(ctx, spec3))
	seed(t, st, "a", "b", "d1")
	repo := ports.NewID()
	require.NoError(t, x.Upsert(ctx, 0, []ports.ChunkVector{
		{ChunkID: "a", Source: ports.SourceCode, Vector: []float32{1, 0, 0}},
		{ChunkID: "b", Source: ports.SourceCode, Vector: []float32{0, 1, 0}},
		{ChunkID: "d1", Source: ports.SourceGeneratedDoc, Vector: []float32{0.9, 0.1, 0}},
	}))
	hits, err := x.Search(ctx, []float32{1, 0, 0}, 2, ports.VectorFilter{})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.Equal(t, "a", hits[0].ChunkID)
	assert.InDelta(t, 1.0, hits[0].Score, 1e-6)
	assert.Equal(t, "d1", hits[1].ChunkID)

	hits, _ = x.Search(ctx, []float32{1, 0, 0}, 5, ports.VectorFilter{Sources: []ports.ChunkSource{ports.SourceGeneratedDoc}})
	require.Len(t, hits, 1)
	assert.Equal(t, "d1", hits[0].ChunkID)
	hits, _ = x.Search(ctx, []float32{1, 0, 0}, 5, ports.VectorFilter{RepoIDs: []string{repo}})
	assert.Empty(t, hits, "repo filter")
	hits, _ = x.Search(ctx, []float32{1, 0, 0}, 0, ports.VectorFilter{})
	assert.Empty(t, hits)

	// Update in place, then soft-delete the chunk: search only returns live chunks.
	require.NoError(t, x.Upsert(ctx, 0, []ports.ChunkVector{{ChunkID: "b", Source: ports.SourceCode, Vector: []float32{1, 0, 0}}}))
	_, err = store.NewChunks(st).Apply(ctx, store.ChunkWrite{Remove: []string{"a"}})
	require.NoError(t, err)
	hits, _ = x.Search(ctx, []float32{1, 0, 0}, 1, ports.VectorFilter{})
	assert.Equal(t, "b", hits[0].ChunkID)

	require.NoError(t, x.Delete(ctx, []string{"b", "d1"}))
	require.NoError(t, x.Delete(ctx, nil))
	hits, _ = x.Search(ctx, []float32{1, 0, 0}, 5, ports.VectorFilter{})
	assert.Empty(t, hits)

	err = x.Upsert(ctx, 0, []ports.ChunkVector{{ChunkID: "a", Source: ports.SourceCode, Vector: []float32{1, 0}}})
	var pe *ports.PermanentError
	assert.ErrorAs(t, err, &pe, "dimension mismatch is permanent")
	assert.ErrorIs(t, x.Upsert(ctx, 9, []ports.ChunkVector{{ChunkID: "a", Vector: []float32{1, 0, 0}}}), ports.ErrNotFound)
	require.NoError(t, x.Upsert(ctx, 0, nil))
}

func TestReindexDualWriteAndSwap(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	x := New(st.Pool)
	require.NoError(t, x.EnsureIndex(ctx, spec3))
	seed(t, st, "a", "b")
	require.NoError(t, x.Upsert(ctx, 0, []ports.ChunkVector{{ChunkID: "a", Source: ports.SourceCode, Vector: []float32{1, 0, 0}}}))

	spec4 := ports.EmbeddingSpec{ProviderKind: "bedrock", Model: "titan", Dimensions: 4}
	v, err := x.BeginReindex(ctx, spec4)
	require.NoError(t, err)
	assert.Equal(t, 2, v)
	again, err := x.BeginReindex(ctx, spec4)
	require.NoError(t, err)
	assert.Equal(t, v, again, "resuming the same reindex")
	_, err = x.BeginReindex(ctx, spec3)
	assert.Error(t, err, "one reindex at a time")

	// Reads still hit the live 3-dim version while the rebuild runs; writes are dual-written.
	require.NoError(t, x.Upsert(ctx, v, []ports.ChunkVector{{ChunkID: "a", Source: ports.SourceCode, Vector: []float32{0, 0, 0, 1}}}))
	require.NoError(t, x.Upsert(ctx, 0, []ports.ChunkVector{{ChunkID: "b", Source: ports.SourceCode, Vector: []float32{0, 1, 0}}}))
	require.NoError(t, x.Upsert(ctx, v, []ports.ChunkVector{{ChunkID: "b", Source: ports.SourceCode, Vector: []float32{0, 0, 1, 0}}}))
	hits, err := x.Search(ctx, []float32{1, 0, 0}, 1, ports.VectorFilter{})
	require.NoError(t, err)
	assert.Equal(t, "a", hits[0].ChunkID)
	s, _ := x.State(ctx)
	assert.Equal(t, v, s.PendingVersion)
	assert.Equal(t, &spec4, s.Pending)

	assert.Error(t, x.SwapReindex(ctx, 7))
	require.NoError(t, x.SwapReindex(ctx, v))
	s, _ = x.State(ctx)
	assert.Equal(t, v, s.Version)
	assert.Equal(t, &spec4, s.Live)
	assert.Nil(t, s.Pending)
	hits, err = x.Search(ctx, []float32{0, 0, 1, 0}, 1, ports.VectorFilter{})
	require.NoError(t, err)
	assert.Equal(t, "b", hits[0].ChunkID)
	assert.ErrorIs(t, x.EnsureIndex(ctx, spec3), ports.ErrEmbeddingMismatch, "startup with the old model now refuses")
	require.NoError(t, x.EnsureIndex(ctx, spec4))

	// Abort leaves the live version untouched.
	v3, err := x.BeginReindex(ctx, spec3)
	require.NoError(t, err)
	require.NoError(t, x.AbortReindex(ctx, v3))
	require.NoError(t, x.AbortReindex(ctx, v3), "aborting twice is a no-op")
	s, _ = x.State(ctx)
	assert.Equal(t, v, s.Version)
	assert.Zero(t, s.PendingVersion)
	hits, _ = x.Search(ctx, []float32{0, 0, 1, 0}, 1, ports.VectorFilter{})
	assert.Equal(t, "b", hits[0].ChunkID)
}

func TestEncode(t *testing.T) {
	assert.Equal(t, "[1,-0.5,0.25]", Encode([]float32{1, -0.5, 0.25}))
	assert.Equal(t, "[]", Encode(nil))
}

func TestSearchBeforeAnyEmbeddingIsNotFound(t *testing.T) {
	st := storetest.New(t)
	_, err := New(st.Pool).Search(context.Background(), []float32{1, 0}, 5, ports.VectorFilter{})
	assert.ErrorIs(t, err, ports.ErrNotFound, "callers treat an uninitialised index as no hits")
}

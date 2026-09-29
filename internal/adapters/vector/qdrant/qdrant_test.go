package qdrant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/qdrantmock"
)

func TestAliasSwapAndCollections(t *testing.T) {
	st := storetest.New(t)
	m := qdrantmock.New()
	defer m.Close()
	ctx := context.Background()
	x := New(st.Pool, m.URL+"/", "", nil)
	assert.Equal(t, "qdrant", x.Kind())
	spec := ports.EmbeddingSpec{ProviderKind: "openai", Model: "e", Dimensions: 2}
	require.NoError(t, x.EnsureIndex(ctx, spec))
	assert.Equal(t, "dth_chunks_v1", m.Alias("dth_chunks"))
	_, err := store.NewChunks(st).Apply(ctx, store.ChunkWrite{Upserts: []ports.Chunk{{ID: "a", Scope: "s", Source: ports.SourceCode, Path: "p", Symbol: "a", Content: "a", ContentHash: "a"}}})
	require.NoError(t, err)
	require.NoError(t, x.Upsert(ctx, 0, []ports.ChunkVector{{ChunkID: "a", Source: ports.SourceCode, Vector: []float32{1, 0}}}))
	assert.Equal(t, 1, m.Count("dth_chunks_v1"))

	v, err := x.BeginReindex(ctx, ports.EmbeddingSpec{ProviderKind: "openai", Model: "e2", Dimensions: 2})
	require.NoError(t, err)
	require.NoError(t, x.Upsert(ctx, v, []ports.ChunkVector{{ChunkID: "a", Source: ports.SourceCode, Vector: []float32{0, 1}}}))
	require.NoError(t, x.Delete(ctx, []string{"a"}))
	assert.Zero(t, m.Count("dth_chunks_v1"), "deletes reach the live version")
	assert.Zero(t, m.Count("dth_chunks_v2"), "and the pending one")
	require.NoError(t, x.SwapReindex(ctx, v))
	assert.Equal(t, "dth_chunks_v2", m.Alias("dth_chunks"))
	assert.Equal(t, []string{"dth_chunks_v2"}, m.Collections(), "the old collection is dropped")

	// A lost collection (e.g. a wiped Qdrant volume) is recreated on start.
	m2 := qdrantmock.New()
	defer m2.Close()
	y := New(st.Pool, m2.URL, "", nil)
	require.NoError(t, y.EnsureIndex(ctx, ports.EmbeddingSpec{ProviderKind: "openai", Model: "e2", Dimensions: 2}))
	assert.Equal(t, []string{"dth_chunks_v2"}, m2.Collections())
}

func TestPointIDIsStableUUID(t *testing.T) {
	a := PointID("abc")
	assert.Equal(t, a, PointID("abc"))
	assert.NotEqual(t, a, PointID("abd"))
	assert.Len(t, a, 36)
}

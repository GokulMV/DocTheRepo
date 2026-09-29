package parity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/pgvector"
	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/qdrant"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/qdrantmock"
)

type vectorCase struct {
	name  string
	build func(t *testing.T, st *store.Store) ports.VectorIndex
}

var vectorCases = []vectorCase{
	{"pgvector", func(t *testing.T, st *store.Store) ports.VectorIndex { return pgvector.New(st.Pool) }},
	{"qdrant", func(t *testing.T, st *store.Store) ports.VectorIndex {
		m := qdrantmock.New()
		m.APIKey = "k"
		t.Cleanup(m.Close)
		return qdrant.New(st.Pool, m.URL, "k", fastHTTP())
	}},
}

func seedChunks(t *testing.T, st *store.Store, repo string, ids ...string) {
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

// TestVectorIndexParity runs the same search, soft-delete, and reindex behavior against every index.
func TestVectorIndexParity(t *testing.T) {
	spec3 := ports.EmbeddingSpec{ProviderKind: "openai", Model: "e3", Dimensions: 3}
	spec4 := ports.EmbeddingSpec{ProviderKind: "bedrock", Model: "titan", Dimensions: 4}
	for _, vc := range vectorCases {
		t.Run(vc.name, func(t *testing.T) {
			st := storetest.New(t)
			ctx := context.Background()
			x := vc.build(t, st)
			require.NoError(t, x.EnsureIndex(ctx, spec3))
			require.NoError(t, x.EnsureIndex(ctx, spec3))
			assert.ErrorIs(t, x.EnsureIndex(ctx, spec4), ports.ErrEmbeddingMismatch)
			repo := ports.NewID()
			seedChunks(t, st, repo, "a", "b", "d1")
			require.NoError(t, x.Upsert(ctx, 0, []ports.ChunkVector{
				{ChunkID: "a", RepoID: repo, Source: ports.SourceCode, Vector: []float32{1, 0, 0}},
				{ChunkID: "b", Source: ports.SourceCode, Vector: []float32{0, 1, 0}},
				{ChunkID: "d1", Source: ports.SourceGeneratedDoc, Vector: []float32{0.9, 0.1, 0}},
			}))
			hits, err := x.Search(ctx, []float32{1, 0, 0}, 2, ports.VectorFilter{})
			require.NoError(t, err)
			require.Len(t, hits, 2)
			assert.Equal(t, []string{"a", "d1"}, []string{hits[0].ChunkID, hits[1].ChunkID})
			assert.InDelta(t, 1.0, hits[0].Score, 1e-5)
			hits, _ = x.Search(ctx, []float32{1, 0, 0}, 5, ports.VectorFilter{Sources: []ports.ChunkSource{ports.SourceGeneratedDoc}})
			require.Len(t, hits, 1)
			assert.Equal(t, "d1", hits[0].ChunkID)
			hits, _ = x.Search(ctx, []float32{0, 1, 0}, 5, ports.VectorFilter{RepoIDs: []string{repo}})
			require.Len(t, hits, 1)
			assert.Equal(t, "a", hits[0].ChunkID)

			_, err = store.NewChunks(st).Apply(ctx, store.ChunkWrite{Remove: []string{"a"}})
			require.NoError(t, err)
			hits, _ = x.Search(ctx, []float32{1, 0, 0}, 1, ports.VectorFilter{})
			require.Len(t, hits, 1)
			assert.Equal(t, "d1", hits[0].ChunkID, "soft-deleted chunks never surface")

			err = x.Upsert(ctx, 0, []ports.ChunkVector{{ChunkID: "a", Source: ports.SourceCode, Vector: []float32{1, 0}}})
			var pe *ports.PermanentError
			assert.ErrorAs(t, err, &pe)

			v, err := x.BeginReindex(ctx, spec4)
			require.NoError(t, err)
			require.NoError(t, x.Upsert(ctx, v, []ports.ChunkVector{{ChunkID: "b", Source: ports.SourceCode, Vector: []float32{0, 0, 1, 0}}}))
			hits, _ = x.Search(ctx, []float32{0, 1, 0}, 1, ports.VectorFilter{})
			assert.Equal(t, "b", hits[0].ChunkID, "reads stay on the live version during a rebuild")
			require.NoError(t, x.Delete(ctx, []string{"d1"}))
			require.NoError(t, x.SwapReindex(ctx, v))
			hits, err = x.Search(ctx, []float32{0, 0, 1, 0}, 3, ports.VectorFilter{})
			require.NoError(t, err)
			require.Len(t, hits, 1)
			assert.Equal(t, "b", hits[0].ChunkID)
			s, _ := x.State(ctx)
			assert.Equal(t, v, s.Version)
			assert.Equal(t, &spec4, s.Live)

			v2, err := x.BeginReindex(ctx, spec3)
			require.NoError(t, err)
			require.NoError(t, x.AbortReindex(ctx, v2))
			s, _ = x.State(ctx)
			assert.Nil(t, s.Pending)
			hits, _ = x.Search(ctx, []float32{0, 0, 1, 0}, 1, ports.VectorFilter{})
			assert.Equal(t, "b", hits[0].ChunkID)
		})
	}
}

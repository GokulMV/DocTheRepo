package ports

import (
	"context"
	"errors"
)

// ErrEmbeddingMismatch means the configured embedding model/dimensions differ from the index lock; a
// reindex is required before the new model can be used (plan § 8.18).
var ErrEmbeddingMismatch = errors.New("embedding model differs from the locked index: run a reindex")

// EmbeddingSpec identifies the embedding space an index holds.
type EmbeddingSpec struct {
	ProviderKind string `json:"provider_kind"`
	Model        string `json:"model"`
	Dimensions   int    `json:"dimensions"`
}

// ChunkVector is one chunk's embedding.
type ChunkVector struct {
	ChunkID string
	RepoID  string
	Source  ChunkSource
	Vector  []float32
}

// VectorFilter narrows a search. Empty fields do not filter.
type VectorFilter struct {
	RepoIDs []string
	Sources []ChunkSource
}

// VectorHit is one search result (Score: higher is more similar, cosine similarity in [-1, 1]).
type VectorHit struct {
	ChunkID string  `json:"chunk_id"`
	Score   float64 `json:"score"`
}

// IndexState reports the live and pending (reindex) embedding spaces.
type IndexState struct {
	Live    *EmbeddingSpec `json:"live,omitempty"`
	Version int            `json:"version"`
	Pending *EmbeddingSpec `json:"pending,omitempty"`
	// PendingVersion is the version being rebuilt, 0 when no reindex runs.
	PendingVersion int `json:"pending_version,omitempty"`
}

// VectorIndex stores and searches chunk embeddings. Writes go to the live version and, while a reindex
// runs, are dual-written to the pending one by the caller (Upsert with the pending spec).
type VectorIndex interface {
	Kind() string
	// EnsureIndex creates the index on first use and locks spec; a different spec returns ErrEmbeddingMismatch.
	EnsureIndex(ctx context.Context, spec EmbeddingSpec) error
	State(ctx context.Context) (IndexState, error)
	// Upsert writes vectors to version (0 = live).
	Upsert(ctx context.Context, version int, vs []ChunkVector) error
	Delete(ctx context.Context, chunkIDs []string) error
	// Rekey copies vectors from old to new chunk IDs (a structurally unchanged rename: no re-embedding),
	// in the live and any pending version. The new chunks must already exist; old vectors are kept.
	Rekey(ctx context.Context, oldToNew map[string]string) error
	Search(ctx context.Context, vec []float32, k int, f VectorFilter) ([]VectorHit, error)
	// BeginReindex creates the pending version for spec and returns its number.
	BeginReindex(ctx context.Context, spec EmbeddingSpec) (int, error)
	// SwapReindex atomically makes the pending version live and drops the old one.
	SwapReindex(ctx context.Context, version int) error
	// AbortReindex drops the pending version and leaves the live one untouched.
	AbortReindex(ctx context.Context, version int) error
}

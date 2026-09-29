package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// MaxEmbedChars caps the text embedded per chunk (embedding models truncate anyway; this keeps requests
// and spend estimates bounded).
const MaxEmbedChars = 8000

// Indexer embeds chunks on the embedding route and writes them to the vector index version whose
// embedding space matches the route: the live one normally, the pending one while a reindex to the
// route's new model runs (the reindex job's catch-up pass then covers the live gap until the swap).
type Indexer struct {
	GW    *llmgateway.Gateway
	Index ports.VectorIndex
}

// EmbedText is what gets embedded for a chunk: its location and symbol give short chunks context.
func EmbedText(c ports.Chunk) string {
	var b strings.Builder
	b.WriteString(c.Path)
	if c.Symbol != "" {
		b.WriteString(" · " + c.Symbol)
	}
	b.WriteString("\n")
	if c.Signature != "" {
		b.WriteString(c.Signature + "\n")
	}
	b.WriteString(c.Content)
	s := b.String()
	if len(s) > MaxEmbedChars {
		s = s[:MaxEmbedChars]
	}
	return s
}

// Embed embeds and upserts chunks. It returns (0, nil) when no embedding route is configured: chunks
// stay searchable by full text until one is.
func (x *Indexer) Embed(ctx context.Context, meta llmgateway.CallMeta, chunks []ports.Chunk) (int, error) {
	return x.EmbedVersion(ctx, meta, chunks, -1)
}

// EmbedVersion embeds into a specific version (reindex), or -1 to pick the version matching the route.
func (x *Indexer) EmbedVersion(ctx context.Context, meta llmgateway.CallMeta, chunks []ports.Chunk, version int) (int, error) {
	if len(chunks) == 0 || x == nil || x.Index == nil {
		return 0, nil
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = EmbedText(c)
	}
	vecs, route, err := x.GW.Embed(ctx, meta, texts)
	if errors.Is(err, llmgateway.ErrNoRoute) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	spec := ports.EmbeddingSpec{ProviderKind: route.ProviderKind, Model: route.Model, Dimensions: len(vecs[0])}
	if version < 0 {
		if version, err = x.versionFor(ctx, spec); err != nil {
			return 0, err
		}
	}
	vs := make([]ports.ChunkVector, len(chunks))
	for i, c := range chunks {
		vs[i] = ports.ChunkVector{ChunkID: c.ID, RepoID: c.RepoID, Source: c.Source, Vector: vecs[i]}
	}
	if err := x.Index.Upsert(ctx, version, vs); err != nil {
		return 0, err
	}
	return len(vs), nil
}

// versionFor maps an embedding space to the index version holding it, initialising the index on first use.
func (x *Indexer) versionFor(ctx context.Context, spec ports.EmbeddingSpec) (int, error) {
	st, err := x.Index.State(ctx)
	if err != nil {
		return 0, err
	}
	switch {
	case st.Live == nil:
		if err := x.Index.EnsureIndex(ctx, spec); err != nil {
			return 0, err
		}
		return 0, nil
	case *st.Live == spec:
		return 0, nil
	case st.Pending != nil && *st.Pending == spec:
		return st.PendingVersion, nil
	}
	return 0, ports.Permanent(fmt.Errorf("embedding route is %s/%s (%d dims) but the index holds %s/%s: start a reindex: %w",
		spec.ProviderKind, spec.Model, spec.Dimensions, st.Live.ProviderKind, st.Live.Model, ports.ErrEmbeddingMismatch))
}

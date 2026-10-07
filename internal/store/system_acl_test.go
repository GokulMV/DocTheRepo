package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// The System architecture covers several repositories: its pieces are found only by someone who can read
// all of them, through every way Ask reads the index.
func TestSystemPiecesNeedEveryRepository(t *testing.T) {
	st, conns, cid, shop := fixture(t)
	_ = conns
	ctx := context.Background()
	lib, err := store.NewRepos(st).Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: "acme/lib", DefaultBranch: "main"})
	require.NoError(t, err)
	d := repodocs.Doc{Type: "system", Key: "system", Title: "System architecture", Status: "ok", Confidence: 0.9, AtAGlance: "The shop uses the ledger library for every refund.",
		Sections: []repodocs.DocSection{{Key: "flows", Title: "End-to-end flows", Markdown: "Refund requests go from the shop to the ledger library."}}}
	pieces := repodocs.SystemChunks(d, []string{shop, lib})
	require.Len(t, pieces, 2)
	chunks := store.NewChunks(st)
	_, err = chunks.Apply(ctx, ports.ChunkWrite{Upserts: pieces})
	require.NoError(t, err)

	qa := store.NewQA(st)
	both := rag.Scope{RepoIDs: []string{shop, lib}}
	onlyShop := rag.Scope{RepoIDs: []string{shop}}
	ids := []string{pieces[0].ID, pieces[1].ID}

	hits, err := qa.FullText(ctx, "ledger refund", both, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, hits, "readers of both repositories find it")
	hits, err = qa.FullText(ctx, "ledger refund", onlyShop, 10)
	require.NoError(t, err)
	assert.Empty(t, hits, "a reader of one repository does not")
	hits, err = qa.FullText(ctx, "ledger refund", rag.Scope{All: true}, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, hits, "admins read everything")

	got, err := qa.Chunks(ctx, ids, onlyShop)
	require.NoError(t, err)
	assert.Empty(t, got, "loading by id (after a vector search) is filtered too")
	got, err = qa.Chunks(ctx, ids, both)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.ElementsMatch(t, []string{shop, lib}, got[0].RequiresRepos)

	got, err = qa.ChunksForPath(ctx, repodocs.SystemPath, onlyShop, 10)
	require.NoError(t, err)
	assert.Empty(t, got, "the agent cannot read it as a file")
	paths, err := qa.ListPaths(ctx, "system", onlyShop, 10)
	require.NoError(t, err)
	assert.Empty(t, paths, "nor list it")
	paths, err = qa.ListPaths(ctx, "system", both, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, paths)

	// Issue explanations never use it, whoever sees the issue.
	dec, err := store.NewDecodes(st, chunks).Chunks(ctx, ids)
	require.NoError(t, err)
	assert.Empty(t, dec)
}

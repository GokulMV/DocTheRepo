package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// Configuration and CI facts link repositories in the System view: a host one calls that another serves,
// and a pipeline that uses another repository's workflow.
func TestWiringLinksRepositories(t *testing.T) {
	st, _, cid, shop := fixture(t)
	ctx := context.Background()
	billing, err := store.NewRepos(st).Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: "acme/billing", DefaultBranch: "main"})
	require.NoError(t, err)
	rd := store.NewRepoDocs(st)

	shopWires := []repodocs.Wire{
		{Kind: "calls", Value: "billing.payments.svc.cluster.local", Note: "http://billing.payments.svc.cluster.local:8080", Path: "deploy/app.yaml", Line: 12},
		{Kind: "ci_ref", Value: "acme/billing", Note: "uses: acme/billing/.github/workflows/contract.yml@main", Path: ".github/workflows/ci.yml", Line: 7},
	}
	changed, err := rd.PutWiring(ctx, shop, shopWires)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = rd.PutWiring(ctx, shop, shopWires)
	require.NoError(t, err)
	assert.False(t, changed, "the same facts again are not a change")

	links, err := rd.SystemLinks(ctx)
	require.NoError(t, err)
	got := map[string]repodocs.SystemLink{}
	for _, l := range links {
		got[l.Kind] = l
	}
	require.Contains(t, got, "api", "a cluster name after the repository links them: %+v", links)
	assert.Equal(t, shop, got["api"].FromRepo)
	assert.Equal(t, billing, got["api"].ToRepo)
	assert.Equal(t, "deploy/app.yaml", got["api"].Path)
	assert.Equal(t, 12, got["api"].Line)
	require.Contains(t, got, "pipeline")
	assert.Equal(t, "uses: acme/billing/.github/workflows/contract.yml@main", got["pipeline"].Via)

	changed, err = rd.PutWiring(ctx, shop, nil)
	require.NoError(t, err)
	assert.True(t, changed)
	links, err = rd.SystemLinks(ctx)
	require.NoError(t, err)
	assert.Empty(t, links, "facts removed, links gone")
}

// Go imports of the repository's own packages reach the Docs v2 facts, so a package used only through
// method calls on its types still has its users.
func TestRepoDocsFactsReadImports(t *testing.T) {
	st, _, _, shop := fixture(t)
	ctx := context.Background()
	a := &chunker.FileAnalysis{Language: "go", Imports: []chunker.Import{
		{Path: "acme.dev/shop/internal/mcpconn", Line: 4}, {Path: "context", Line: 3},
	}}
	g := palace.ExtractImports("acme/shop", "internal/api/server.go", "c1", a, "acme.dev/shop")
	require.NoError(t, store.NewGraph(st).ReplaceSource(ctx, shop, "internal/api/server.go", g))

	f, err := store.NewRepoDocs(st).Facts(ctx, shop)
	require.NoError(t, err)
	assert.Equal(t, []repodocs.Import{{From: "internal/api/server.go", Dir: "internal/mcpconn"}}, f.Imports)
}

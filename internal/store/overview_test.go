package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

func TestPalaceOverview(t *testing.T) {
	ctx := context.Background()
	st, _, _, repoID := fixture(t)
	var g palace.Graph
	g.AddEntity(palace.Entity{Ref: palace.RepoRef("acme/shop"), Name: "acme/shop", Repo: "acme/shop"})
	sym := g.AddEntity(palace.Entity{Ref: palace.SymbolRef("acme/shop", "orders.go", "Publish"), Name: "Publish", Repo: "acme/shop"})
	g.AddEdge(palace.RepoRef("acme/shop"), palace.EdgeContains, sym, palace.Evidence{})
	g.AddEdge(sym, palace.EdgePublishes, palace.TopicRef("orders"), palace.Evidence{Path: "orders.go"})
	g.AddEdge(palace.ServiceRef("shop"), palace.EdgeDeployedAs, palace.RepoRef("acme/shop"), palace.Evidence{})
	require.NoError(t, store.NewGraph(st).ReplaceSource(ctx, repoID, "orders.go", g))

	b := store.NewBrowse(st)
	o, err := b.Overview(ctx, nil, 300, rag.Scope{All: true})
	require.NoError(t, err)
	names := map[string]string{}
	for _, n := range o.Nodes {
		names[n.ID] = n.Kind + ":" + n.Name
	}
	assert.ElementsMatch(t, []string{"repo:acme/shop", "queue_topic:orders", "service:shop"}, values(names), "symbols are not on the map")
	var lifted []string
	for _, e := range o.Edges {
		lifted = append(lifted, names[e.Src]+" -"+e.Kind+"-> "+names[e.Dst])
	}
	assert.ElementsMatch(t, []string{"repo:acme/shop -publishes-> queue_topic:orders", "service:shop -deployed_as-> repo:acme/shop"}, lifted,
		"the symbol's publish edge is lifted to its repository; contains edges are not")
	assert.Equal(t, 1, o.Counts["symbol"])
	assert.False(t, o.Truncated)

	o, err = b.Overview(ctx, []string{"repo"}, 1, rag.Scope{RepoIDs: []string{"00000000-0000-0000-0000-000000000009"}})
	require.NoError(t, err)
	assert.Empty(t, o.Nodes, "repo-scoped readers see only their repositories' entities")
	o, err = b.Overview(ctx, []string{"repo", "service", "queue_topic"}, 1, rag.Scope{All: true})
	require.NoError(t, err)
	assert.Len(t, o.Nodes, 1)
	assert.True(t, o.Truncated)
}

func values(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

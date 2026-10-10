package repodocs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleGraph_ImportsMakeUsedBy(t *testing.T) {
	// internal/api reaches internal/mcpconn only through a method on a struct field (c.client.Call),
	// which leaves no call edge; its import of the package still makes the dependency.
	f := &Facts{Files: []File{
		{Path: "internal/api/server.go", Lines: 600}, {Path: "internal/api/routes.go", Lines: 400},
		{Path: "internal/mcpconn/client.go", Lines: 700},
		{Path: "internal/store/store.go", Lines: 800},
	}, Imports: []Import{
		{From: "internal/api/server.go", Dir: "internal/mcpconn"},
		{From: "internal/api/server.go", Dir: "internal/mcpconn"}, // read twice: counted once
		{From: "internal/api/routes.go", Dir: "internal/mcpconn"},
		{From: "internal/api/routes.go", Dir: "internal/store"},
		{From: "internal/api/routes.go", Dir: "internal/gone"}, // no indexed files: ignored
		{From: "internal/mcpconn/client.go", Dir: "internal/mcpconn"},
	}, Calls: []Call{{From: "internal/api/routes.go", To: "internal/store/store.go", N: 3}}}
	mods := Split(f, SplitOptions{MinLines: 100, MaxLines: 1000, Target: 25})
	e := newEnv(f, mods, nil, nil)
	key := map[string]string{}
	for _, m := range mods {
		key[m.Dir] = m.Key
	}
	require.NotEmpty(t, key["internal/mcpconn"])
	require.NotEmpty(t, key["internal/api"])

	assert.Equal(t, map[[2]string]modEdge{
		{key["internal/api"], key["internal/mcpconn"]}: {files: 2},
		{key["internal/api"], key["internal/store"]}:   {calls: 3, files: 1},
	}, e.modEdges, "one edge per module pair, imports and calls together")

	in := e.inputs(context.Background(), Spec{Needs: []string{"module_graph"}}, e.module(key["internal/mcpconn"]), 100000)
	assert.Contains(t, in, "- used by internal/api (2 importing files)")
	in = e.inputs(context.Background(), Spec{Needs: []string{"module_graph"}}, e.module(key["internal/api"]), 100000)
	assert.Contains(t, in, "- uses internal/mcpconn (2 importing files)")
	assert.Contains(t, in, "- uses internal/store (1 importing file, 3 calls)")
	text, diagram := e.moduleGraph()
	assert.Contains(t, text, "internal/api → internal/store (1 importing file, 3 calls)")
	assert.Contains(t, diagram, mermaidID(key["internal/api"])+" --> "+mermaidID(key["internal/mcpconn"]))
}

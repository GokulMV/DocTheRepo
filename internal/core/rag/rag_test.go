package rag

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestFuse(t *testing.T) {
	a := []ports.VectorHit{{ChunkID: "x"}, {ChunkID: "y"}, {ChunkID: "z"}}
	b := []ports.VectorHit{{ChunkID: "y"}, {ChunkID: "w"}}
	got := Fuse(60, a, b)
	require.Len(t, got, 4)
	assert.Equal(t, "y", got[0].ChunkID, "found by both methods ranks first")
	assert.InDelta(t, 1.0/62+1.0/61, got[0].Score, 1e-12)
	assert.Empty(t, Fuse(60))
}

func TestPackBudgetAndPerFileCap(t *testing.T) {
	var cs []ports.Chunk
	for i := range 5 {
		cs = append(cs, ports.Chunk{ID: string(rune('a' + i)), Scope: "r", Path: "same.go", Content: "func f() {}"})
	}
	cs = append(cs, ports.Chunk{ID: "big", Scope: "r", Path: "big.go", Content: strings.Repeat("word ", 5000)}, ports.Chunk{ID: "o", Scope: "r", Path: "o.go", Content: "x"})
	got := Pack(cs, 500)
	ids := []string{}
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"a", "b", "c", "o"}, ids, "3 per file; an oversized chunk is skipped, smaller later ones still fit")
}

func TestCiteDropsFabricatedAndRenumbers(t *testing.T) {
	supplied := []ports.Chunk{
		{ID: "c1", Scope: "acme/shop", Path: "a.go", Symbol: "A", Source: ports.SourceCode},
		{ID: "c2", Scope: "acme/shop", Path: "docs/b.md", Symbol: "__intro__", Source: ports.SourceGeneratedDoc},
	}
	text, cits := Cite("B does it [2]. A too [1][2]. Invented [7].", supplied)
	assert.Equal(t, "B does it [1]. A too [2][1]. Invented .", text)
	require.Len(t, cits, 2)
	assert.Equal(t, Citation{N: 1, Type: "doc", Title: "docs/b.md", Repo: "acme/shop", Path: "docs/b.md", ChunkID: "c2"}, cits[0])
	assert.Equal(t, "code", cits[1].Type)
	_, none := Cite("no citations", supplied)
	assert.Empty(t, none)
}

func TestCacheKeyAndSources(t *testing.T) {
	s := Scope{RepoIDs: []string{"b", "a"}, Sources: []ports.ChunkSource{ports.SourceCode}}
	k1 := CacheKey("  How  does refund WORK? ", s, 3)
	assert.Equal(t, k1, CacheKey("how does refund work?", Scope{RepoIDs: []string{"a", "b"}, Sources: s.Sources}, 3))
	assert.NotEqual(t, k1, CacheKey("how does refund work?", s, 4), "index version invalidates")
	assert.NotEqual(t, k1, CacheKey("how does refund work?", Scope{All: true}, 3), "scope is part of the key")
	src, err := Sources([]string{"code", "docs"})
	require.NoError(t, err)
	assert.Equal(t, []ports.ChunkSource{ports.SourceCode, ports.SourceGeneratedDoc, ports.SourceImportedDoc}, src)
	_, err = Sources([]string{"slack"})
	assert.Error(t, err)
	assert.Contains(t, Prompt("q?", []ports.Chunk{{Scope: "r", Path: "p", Content: "c"}}), `<source n="1"`)
}

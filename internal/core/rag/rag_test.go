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

func TestFingerprint(t *testing.T) {
	for q, want := range map[string]string{
		"Can you explain how the payment retries work?": "how payment retry work",
		"how payment retry works":                       "how payment retry work",
		"What does src/api/server.go do?":               "what src/api/server.go",
		"Who calls ChargeCard?":                         "who call chargecard",
		"does the class handle process errors":          "class handle process error",
		"?!":                                            "",
	} {
		assert.Equal(t, want, Fingerprint(q), q)
	}
	assert.Equal(t, Fingerprint("Can you explain how the payment retries work?"), Fingerprint("how payment retry works"))
	assert.NotEqual(t, Fingerprint("does A call B"), Fingerprint("does B call A"), "word order is kept")
}

func TestCacheKeyAndSources(t *testing.T) {
	s := Scope{RepoIDs: []string{"b", "a"}, Sources: []ports.ChunkSource{ports.SourceCode}}
	k1 := CacheKey("  How  does refund WORK? ", s)
	assert.Equal(t, k1, CacheKey("how does refund work?", Scope{RepoIDs: []string{"a", "b"}, Sources: s.Sources}))
	assert.Equal(t, k1, CacheKey("Can you please explain how the refunds work", s), "rephrasing with filler hits the same entry")
	assert.NotEqual(t, k1, CacheKey("why does refund work?", s), "the question word matters")
	assert.NotEqual(t, k1, CacheKey("how does refund work?", Scope{All: true}), "scope is part of the key")
	src, err := Sources([]string{"code", "docs"})
	require.NoError(t, err)
	assert.Equal(t, []ports.ChunkSource{ports.SourceCode, ports.SourceGeneratedDoc, ports.SourceImportedDoc, ports.SourceUpload}, src, "uploaded documents count as docs")
	src, err = Sources([]string{"knowledge"})
	require.NoError(t, err)
	assert.Equal(t, ports.SharedSources, src)
	_, err = Sources([]string{"slack"})
	assert.Error(t, err)
	assert.Contains(t, Prompt("q?", []ports.Chunk{{Scope: "r", Path: "p", Content: "c"}}), `<source n="1"`)
}

func TestIsOverviewQuestion(t *testing.T) {
	for q, want := range map[string]bool{
		"explain me about how this repo works":      true,
		"explain how the docthe repo works":         true,
		"What does this service do?":                true,
		"give me an overview of the project":        true,
		"how is the system structured":              true,
		"Why does HandleChargeback freeze refunds?": false,
		"where is the rate limiter configured":      false,
	} {
		assert.Equal(t, want, IsOverviewQuestion(q), q)
	}
}

func TestSemanticHelpers(t *testing.T) {
	assert.InDelta(t, 1.0, Cosine([]float32{1, 2}, []float32{2, 4}), 1e-9)
	assert.InDelta(t, 0.0, Cosine([]float32{1, 0}, []float32{0, 1}), 1e-9)
	assert.Equal(t, 0.0, Cosine([]float32{1}, []float32{1, 2}), "different sizes never match")
	assert.Equal(t, []string{"payretry", "src/pay.go"}, Identifiers("How does PayRetry in src/pay.go work?"))
	assert.Empty(t, Identifiers("How do payment retries work?"), "plain words and a capitalized first word are not identifiers")
	assert.True(t, sameIdentifiers("explain PayRetry", "how does payretry work"))
	assert.False(t, sameIdentifiers("how does PayRetry work", "how does CartRetry work"))
	assert.False(t, sameIdentifiers("what calls retry_payment", "what calls the retry"))
	assert.Equal(t, ScopeKey(Scope{RepoIDs: []string{"b", "a"}}), ScopeKey(Scope{RepoIDs: []string{"a", "b"}}))
	assert.NotEqual(t, ScopeKey(Scope{All: true}), ScopeKey(Scope{RepoIDs: []string{"a"}}))
}

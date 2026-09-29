package docassembly

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	idA = "aaaaaaaaaaaaaaaa"
	idB = "bbbbbbbbbbbbbbbb"
	idC = "cccccccccccccccc"
)

func TestPaths(t *testing.T) {
	assert.Equal(t, "docs/generated/internal/core/triage.go.md", DocPath("docs/generated/", "internal/core/triage.go"))
	assert.Equal(t, "docs/generated/internal/core/README.md", IndexPath("docs/generated", "internal/core/"))
	assert.Equal(t, "docs/generated/README.md", IndexPath("docs/generated/", "."))
}

func TestAssemble_NewFile(t *testing.T) {
	out, changed := Assemble(nil, Update{
		SourcePath: "orders/order.go", Summary: "Order lifecycle.",
		Order: []string{idA, idB},
		Upserts: []Section{
			{ChunkID: idB, Symbol: "Order.Refund", Body: "Refunds an order.\n\n# Details\nIdempotent."},
			{ChunkID: idA, Symbol: "Order", Body: "An order."},
		},
	})
	require.True(t, changed)
	s := string(out)
	assert.True(t, strings.HasPrefix(s, "<!-- dth:generated source=\"orders/order.go\""))
	assert.Contains(t, s, "# `orders/order.go`\n\nOrder lifecycle.\n")
	assert.Less(t, strings.Index(s, "## `Order`"), strings.Index(s, "## `Order.Refund`"), "source order")
	assert.Contains(t, s, "### Details", "H1 in a body is demoted so it cannot break sections")
	assert.Contains(t, s, "<!-- dth:chunk "+idA+" -->\n## `Order`")
}

func TestAssemble_SurgicalUpdatePreservesHumanBlocks(t *testing.T) {
	v1, _ := Assemble(nil, Update{SourcePath: "a.go", Summary: "S", Order: []string{idA, idB, idC}, Upserts: []Section{
		{ChunkID: idA, Symbol: "A", Body: "a v1"}, {ChunkID: idB, Symbol: "B", Body: "b v1"}, {ChunkID: idC, Symbol: "C", Body: "c v1"},
	}})
	// A human adds notes to B and to the file header.
	edited := strings.Replace(string(v1), "b v1\n", "b v1\n\n"+humanOpen+"\nWatch out: B retries twice.\n"+humanClose+"\n", 1)
	edited = strings.Replace(edited, "\nS\n", "\nS\n\n"+humanOpen+"\nOwned by team payments.\n"+humanClose+"\n", 1)

	v2, changed := Assemble([]byte(edited), Update{SourcePath: "a.go", Order: []string{idA, idB, idC},
		Upserts: []Section{{ChunkID: idB, Symbol: "B", Body: "b v2 " + humanOpen + "smuggled" + humanClose}}})
	require.True(t, changed)
	s := string(v2)
	assert.Contains(t, s, "b v2")
	assert.NotContains(t, s, "b v1")
	assert.Contains(t, s, "Watch out: B retries twice.", "human block in a regenerated section survives")
	assert.Contains(t, s, "Owned by team payments.", "human block in the header survives")
	assert.Contains(t, s, "a v1")
	assert.Contains(t, s, "c v1")
	assert.NotContains(t, s, "smuggled", "generated content cannot inject human blocks")
	assert.Contains(t, s, "\nS\n", "summary kept when not provided")

	again, changed := Assemble(v2, Update{SourcePath: "a.go", Order: []string{idA, idB, idC}})
	assert.False(t, changed, "no-op updates are idempotent")
	assert.Equal(t, s, string(again))
}

func TestAssemble_RemoveKeepsHumanNotesInHeader(t *testing.T) {
	v1, _ := Assemble(nil, Update{SourcePath: "a.go", Order: []string{idA, idB}, Upserts: []Section{
		{ChunkID: idA, Symbol: "A", Body: "a"}, {ChunkID: idB, Symbol: "B", Body: "b"},
	}})
	withNote := strings.Replace(string(v1), "\nb\n", "\nb\n\n"+humanOpen+"\nnote on B\n"+humanClose+"\n", 1)
	v2, _ := Assemble([]byte(withNote), Update{SourcePath: "a.go", Removes: []string{idB}})
	s := string(v2)
	assert.NotContains(t, s, "## `B`")
	assert.Contains(t, s, "note on B", "notes on a removed symbol move to the header instead of being lost")
	assert.Less(t, strings.Index(s, "note on B"), strings.Index(s, "## `A`"))
}

func TestAssemble_OrderingRules(t *testing.T) {
	v1, _ := Assemble(nil, Update{SourcePath: "a.go", Order: []string{idA, idB}, Upserts: []Section{
		{ChunkID: idA, Symbol: "A", Body: "a"}, {ChunkID: idB, Symbol: "B", Body: "b"},
	}})
	// Reorder via Order; add C without placing it (goes last).
	v2, _ := Assemble(v1, Update{SourcePath: "a.go", Order: []string{idB, idA}, Upserts: []Section{{ChunkID: idC, Symbol: "C", Body: "c"}}})
	s := string(v2)
	assert.Less(t, strings.Index(s, "## `B`"), strings.Index(s, "## `A`"))
	assert.Less(t, strings.Index(s, "## `A`"), strings.Index(s, "## `C`"))
}

func TestParse_RoundTripsSymbolsAndSummary(t *testing.T) {
	v1, _ := Assemble(nil, Update{SourcePath: "x.py", Summary: "Line one.\nLine two.", Upserts: []Section{{ChunkID: idA, Symbol: "f", Body: "does f"}}})
	d := parse(string(v1))
	require.Len(t, d.sections, 1)
	assert.Equal(t, "f", d.sections[0].symbol)
	assert.Equal(t, "does f", d.sections[0].generated)
	assert.Equal(t, "Line one.\nLine two.", Summary(v1))
	assert.Empty(t, parse("").sections)
}

func TestRenderIndex(t *testing.T) {
	existing := "old\n" + humanOpen + "\nStart with order.go.md\n" + humanClose + "\n"
	out, changed := RenderIndex([]byte(existing), "orders", []IndexEntry{
		{Name: "refund.go", Summary: "Refund flow.\nMore detail."},
		{Name: "order.go", Summary: strings.Repeat("x", 300)},
		{Name: "internal", IsDir: true},
	})
	require.True(t, changed)
	s := string(out)
	assert.Contains(t, s, "# `orders`")
	assert.Contains(t, s, "- [`internal`](internal/README.md)\n- [`order.go`](order.go.md) — ")
	assert.Contains(t, s, "- [`refund.go`](refund.go.md) — Refund flow.\n")
	assert.Contains(t, s, "...", "long summaries are truncated to one line")
	assert.Contains(t, s, "Start with order.go.md")
	root, _ := RenderIndex(nil, "", nil)
	assert.Contains(t, string(root), "(repository root)")
}

func TestParseIndexRoundTrip(t *testing.T) {
	entries := []IndexEntry{{Name: "api", IsDir: true}, {Name: "main.go", Summary: "Entry point."}, {Name: "util.go"}}
	out, _ := RenderIndex(nil, "cmd", entries)
	assert.Equal(t, entries, ParseIndex(out))
	assert.Empty(t, ParseIndex([]byte("# nothing\n- not an entry\n")))
}

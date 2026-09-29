package manifest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func ch(id, path, hash string, src ports.ChunkSource) ports.Chunk {
	return ports.Chunk{ID: id, Path: path, ContentHash: hash, Source: src}
}

func deleted(c ports.Chunk) ports.Chunk {
	now := time.Now()
	c.DeletedAt = &now
	return c
}

func ids(cs []ports.Chunk) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestDiff_AddedChangedRemovedUnchanged(t *testing.T) {
	stored := []ports.Chunk{
		ch("keep", "a.go", "h1", ports.SourceCode),
		ch("edit", "a.go", "h2", ports.SourceCode),
		ch("gone", "a.go", "h3", ports.SourceCode),
		ch("other", "b.go", "h4", ports.SourceCode), // path not affected: never removed
	}
	fresh := []ports.Chunk{
		ch("keep", "a.go", "h1", ports.SourceCode),
		ch("edit", "a.go", "h2b", ports.SourceCode),
		ch("new", "a.go", "h5", ports.SourceCode),
	}
	d := Diff(stored, fresh, []string{"a.go"})
	assert.Equal(t, []string{"new"}, ids(d.Added))
	assert.Equal(t, []string{"edit"}, ids(d.Changed))
	assert.Equal(t, []string{"gone"}, d.Removed)
	assert.Equal(t, 1, d.Unchanged)
	a, c, r := d.Counts()
	assert.Equal(t, [3]int{1, 1, 1}, [3]int{a, c, r})
	assert.Equal(t, []string{"new", "edit"}, ids(d.NeedsEmbedding()))
	assert.False(t, d.Empty())
}

func TestDiff_DeletedFileRemovesAllItsChunks(t *testing.T) {
	stored := []ports.Chunk{ch("x", "gone.go", "h", ports.SourceCode), ch("y", "gone.go", "h", ports.SourceCode)}
	d := Diff(stored, nil, []string{"gone.go"})
	assert.Equal(t, []string{"x", "y"}, d.Removed)
}

func TestDiff_RevertRevivesWithoutReembedding(t *testing.T) {
	stored := []ports.Chunk{deleted(ch("f", "a.go", "h1", ports.SourceCode)), deleted(ch("g", "a.go", "h1", ports.SourceCode))}
	fresh := []ports.Chunk{ch("f", "a.go", "h1", ports.SourceCode), ch("g", "a.go", "DIFFERENT", ports.SourceCode)}
	d := Diff(stored, fresh, []string{"a.go"})
	assert.Equal(t, []string{"f"}, d.Revived)
	assert.Equal(t, []string{"g"}, ids(d.Added), "a revived chunk with new content is re-embedded")
	assert.Empty(t, d.Removed, "already-deleted chunks are not deleted again")
	a, _, _ := d.Counts()
	assert.Equal(t, 2, a)
}

func TestDiff_SourcePrecedence(t *testing.T) {
	stored := []ports.Chunk{
		ch("gen", "docs/a.md", "h1", ports.SourceGeneratedDoc),
		ch("imp", "docs/b.md", "h2", ports.SourceImportedDoc),
	}
	fresh := []ports.Chunk{
		ch("gen", "docs/a.md", "h9", ports.SourceImportedDoc),  // import must not overwrite generated
		ch("imp", "docs/b.md", "h2", ports.SourceGeneratedDoc), // generated supersedes imported even if equal content
	}
	d := Diff(stored, fresh, []string{"docs/a.md", "docs/b.md"})
	assert.Equal(t, []string{"gen"}, d.Skipped)
	assert.Equal(t, []string{"imp"}, ids(d.Changed))
	assert.Empty(t, d.Removed, "a skipped chunk is still 'seen' and not removed")
}

func TestDiff_DuplicateFreshIDsFirstWins_EmptyDelta(t *testing.T) {
	d := Diff(nil, []ports.Chunk{ch("a", "x", "1", ports.SourceCode), ch("a", "x", "2", ports.SourceCode)}, []string{"x"})
	assert.Len(t, d.Added, 1)
	assert.Equal(t, "1", d.Added[0].ContentHash)
	assert.True(t, Diff(nil, nil, nil).Empty())
}

func TestSupersedesAndGC(t *testing.T) {
	assert.True(t, Supersedes(ports.SourceGeneratedDoc, ports.SourceImportedDoc))
	assert.True(t, Supersedes(ports.SourceCode, ports.SourceCode))
	assert.False(t, Supersedes(ports.SourceImportedDoc, ports.SourceGeneratedDoc))
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GCCutoff(now, 14))
}

// Package manifest computes exactly which chunks to upsert or delete after a change (plan § 8.3).
package manifest

import (
	"sort"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Delta is the result of diffing a fresh chunk set against the stored manifest.
type Delta struct {
	Added   []ports.Chunk `json:"added"`
	Changed []ports.Chunk `json:"changed"`
	// Removed are chunk IDs to soft-delete (GC hard-deletes them after the retention window, which lets a
	// quick revert revive them without re-embedding).
	Removed []string `json:"removed"`
	// Revived are soft-deleted chunks that came back with identical content: undelete, no re-embed.
	Revived   []string `json:"revived"`
	Unchanged int      `json:"unchanged"`
	// Skipped are incoming chunks refused by source precedence (an import never overwrites generated docs).
	Skipped []string `json:"skipped,omitempty"`
}

// Counts summarises a delta for job records.
func (d Delta) Counts() (added, changed, removed int) {
	return len(d.Added) + len(d.Revived), len(d.Changed), len(d.Removed)
}

// Empty reports whether the delta requires no writes.
func (d Delta) Empty() bool {
	return len(d.Added) == 0 && len(d.Changed) == 0 && len(d.Removed) == 0 && len(d.Revived) == 0
}

// NeedsEmbedding returns the chunks whose content must be (re-)embedded.
func (d Delta) NeedsEmbedding() []ports.Chunk {
	out := make([]ports.Chunk, 0, len(d.Added)+len(d.Changed))
	out = append(out, d.Added...)
	return append(out, d.Changed...)
}

// Diff compares fresh chunks for the affected paths with what is stored for those paths.
//
//   - fresh ID absent from stored (or stored only as deleted with different content) → Added/Changed
//   - same ID, different content hash → Changed
//   - same ID, soft-deleted, same content hash → Revived
//   - stored live ID on an affected path, absent from fresh → Removed
//
// affected lists every path whose chunks were recomputed (including deleted files, which contribute no
// fresh chunks); chunks on other paths are never removed.
func Diff(stored, fresh []ports.Chunk, affected []string) Delta {
	aff := make(map[string]bool, len(affected))
	for _, p := range affected {
		aff[p] = true
	}
	byID := make(map[string]ports.Chunk, len(stored))
	for _, c := range stored {
		byID[c.ID] = c
	}
	var d Delta
	seen := make(map[string]bool, len(fresh))
	for _, c := range fresh {
		if seen[c.ID] {
			continue // duplicate IDs in one batch: first wins
		}
		seen[c.ID] = true
		old, ok := byID[c.ID]
		switch {
		case !ok:
			d.Added = append(d.Added, c)
		case !Supersedes(c.Source, old.Source):
			d.Skipped = append(d.Skipped, c.ID)
		case !old.Live() && old.ContentHash == c.ContentHash:
			d.Revived = append(d.Revived, c.ID)
		case !old.Live():
			d.Added = append(d.Added, c)
		case old.ContentHash != c.ContentHash || old.Source != c.Source:
			d.Changed = append(d.Changed, c)
		default:
			d.Unchanged++
		}
	}
	for _, c := range stored {
		if c.Live() && aff[c.Path] && !seen[c.ID] {
			d.Removed = append(d.Removed, c.ID)
		}
	}
	sort.Strings(d.Removed)
	sort.Strings(d.Revived)
	return d
}

// Supersedes reports whether a chunk from source incoming may replace one from source existing. Generated
// docs always win over imported docs; an import never overwrites a generated chunk (plan § 6.3).
func Supersedes(incoming, existing ports.ChunkSource) bool {
	return !(incoming == ports.SourceImportedDoc && existing == ports.SourceGeneratedDoc)
}

// GCCutoff returns the time before which soft-deleted chunks are hard-deleted.
func GCCutoff(now time.Time, retentionDays int) time.Time {
	return now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
}

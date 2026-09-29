package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Chunks stores the chunk manifest.
type Chunks struct{ s *Store }

// NewChunks returns the chunk store.
func NewChunks(s *Store) *Chunks { return &Chunks{s: s} }

// ForPaths returns stored chunks (live and soft-deleted) of one source for the given paths.
func (c *Chunks) ForPaths(ctx context.Context, repoID string, source ports.ChunkSource, paths []string) ([]ports.Chunk, error) {
	rows, err := c.s.Q.ChunksForPaths(ctx, gen.ChunksForPathsParams{RepoID: &repoID, Source: gen.ChunkSource(source), Paths: paths})
	if err != nil {
		return nil, fmt.Errorf("load chunks: %w", err)
	}
	return toChunks(rows), nil
}

// ByIDs loads chunks by ID.
func (c *Chunks) ByIDs(ctx context.Context, ids []string) ([]ports.Chunk, error) {
	rows, err := c.s.Q.ChunksByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load chunks: %w", err)
	}
	return toChunks(rows), nil
}

// LiveAfter walks live chunks in ID order (keyset pagination).
func (c *Chunks) LiveAfter(ctx context.Context, after string, limit int) ([]ports.Chunk, error) {
	rows, err := c.s.Q.LiveChunks(ctx, gen.LiveChunksParams{After: after, Lim: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("walk chunks: %w", err)
	}
	return toChunks(rows), nil
}

// ChunkWrite is one transactional manifest update.
type ChunkWrite = ports.ChunkWrite

// Apply writes a manifest update atomically and bumps the index version (answer-cache invalidation).
func (c *Chunks) Apply(ctx context.Context, w ChunkWrite) (int64, error) {
	if w.Empty() {
		return 0, nil
	}
	var version int64
	err := c.s.InTx(ctx, func(q *gen.Queries, _ pgx.Tx) error {
		for _, ch := range w.Upserts {
			if err := q.UpsertChunk(ctx, gen.UpsertChunkParams{ChunkID: ch.ID, RepoID: strPtr(ch.RepoID), Scope: ch.Scope,
				Source: gen.ChunkSource(ch.Source), Path: ch.Path, Symbol: ch.Symbol, Language: ch.Language, Content: ch.Content,
				ContentHash: ch.ContentHash, Signature: ch.Signature, CommitSha: ch.CommitSHA, Url: ch.URL}); err != nil {
				return fmt.Errorf("upsert chunk %s: %w", ch.ID, err)
			}
		}
		if len(w.Revive) > 0 {
			if _, err := q.ReviveChunks(ctx, w.Revive); err != nil {
				return err
			}
		}
		if len(w.Remove) > 0 {
			if _, err := q.SoftDeleteChunks(ctx, w.Remove); err != nil {
				return err
			}
		}
		if len(w.Drop) > 0 {
			if err := q.DeleteChunkIDs(ctx, w.Drop); err != nil {
				return err
			}
		}
		v, err := q.BumpIndexVersion(ctx)
		version = v
		return err
	})
	return version, err
}

// IndexVersion returns the current index version.
func (c *Chunks) IndexVersion(ctx context.Context) (int64, error) { return c.s.Q.GetIndexVersion(ctx) }

func toChunks(rows []gen.Chunk) []ports.Chunk {
	out := make([]ports.Chunk, len(rows))
	for i, r := range rows {
		ch := ports.Chunk{ID: r.ChunkID, Scope: r.Scope, Source: ports.ChunkSource(r.Source), Path: r.Path, Symbol: r.Symbol,
			Language: r.Language, Content: r.Content, ContentHash: r.ContentHash, Signature: r.Signature, CommitSHA: r.CommitSha,
			URL: r.Url, UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt}
		if r.RepoID != nil {
			ch.RepoID = *r.RepoID
		}
		out[i] = ch
	}
	return out
}

// Graph stores the Palace.
type Graph struct{ s *Store }

// NewGraph returns the Palace store.
func NewGraph(s *Store) *Graph { return &Graph{s: s} }

// ReplaceSource replaces everything one source file contributed to the graph: its old edges are retired,
// the new graph's entities and edges upserted, and repo entities no live edge references are retired.
func (g *Graph) ReplaceSource(ctx context.Context, repoID, sourcePath string, gr palace.Graph) error {
	return g.s.InTx(ctx, func(q *gen.Queries, _ pgx.Tx) error {
		if err := q.RetireSourceEdges(ctx, gen.RetireSourceEdgesParams{RepoID: &repoID, SourcePath: sourcePath}); err != nil {
			return err
		}
		ids := map[palace.Ref]string{}
		upsert := func(e palace.Entity) (string, error) {
			if id, ok := ids[e.Ref]; ok {
				return id, nil
			}
			attrs, _ := json.Marshal(orEmpty(e.Attrs))
			var rid *string
			if e.Repo != "" {
				rid = &repoID
			}
			name := e.Name
			if name == "" {
				name = e.Key
			}
			id, err := q.UpsertEntity(ctx, gen.UpsertEntityParams{ID: ports.NewID(), Kind: e.Kind, Key: e.Key, Name: name, RepoID: rid, Attrs: attrs})
			if err != nil {
				return "", fmt.Errorf("upsert entity %s: %w", e.Key, err)
			}
			ids[e.Ref] = id
			return id, nil
		}
		for _, e := range gr.Entities {
			if _, err := upsert(e); err != nil {
				return err
			}
		}
		for _, e := range gr.Edges {
			src, err := upsert(entityOf(gr, e.Src))
			if err != nil {
				return err
			}
			dst, err := upsert(entityOf(gr, e.Dst))
			if err != nil {
				return err
			}
			ev, _ := json.Marshal(e.Evidence)
			if err := q.UpsertEdge(ctx, gen.UpsertEdgeParams{SrcID: src, Kind: e.Kind, DstID: dst, Evidence: ev, RepoID: &repoID, SourcePath: sourcePath}); err != nil {
				return fmt.Errorf("upsert edge: %w", err)
			}
		}
		_, err := q.RetireOrphanEntities(ctx, &repoID)
		return err
	})
}

func entityOf(g palace.Graph, r palace.Ref) palace.Entity {
	if e, ok := g.Find(r); ok {
		return e
	}
	return palace.Entity{Ref: r, Name: r.Key}
}

// EdgeView is an edge as seen from its source entity.
type EdgeView struct {
	Kind string
	Dst  palace.Ref
}

// Edges lists live edges out of an entity.
func (g *Graph) Edges(ctx context.Context, r palace.Ref) ([]EdgeView, error) {
	e, err := g.s.Q.EntityByRef(ctx, gen.EntityByRefParams{Kind: r.Kind, Key: r.Key})
	if IsNoRows(err) {
		return nil, fmt.Errorf("entity %s: %w", r.Key, ports.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	rows, err := g.s.Q.EdgesFrom(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	out := make([]EdgeView, len(rows))
	for i, x := range rows {
		out[i] = EdgeView{Kind: x.Kind, Dst: palace.Ref{Kind: x.DstKind, Key: x.DstKey}}
	}
	return out, nil
}

// DocSection is one section of a generated doc file for the Tree.
type DocSection = ports.DocSection

// Docs stores the Tree (doc_nodes) and Library placements.
type Docs struct {
	s       *Store
	shelves *library.Classifier
}

// NewDocs returns the docs store; shelves classifies doc files onto Library shelves (nil disables it).
func NewDocs(s *Store, shelves *library.Classifier) *Docs { return &Docs{s: s, shelves: shelves} }

// NodeID is the stable ID of a Tree node.
func NodeID(repoID, p, chunkID string) string {
	h := sha256.Sum256([]byte(repoID + "::" + p + "::" + chunkID))
	return hex.EncodeToString(h[:])[:16]
}

// ReplaceFile writes one doc file and its sections into the Tree, creating directory nodes as needed,
// and places the file on Library shelves.
func (d *Docs) ReplaceFile(ctx context.Context, repoID, repoName string, f ports.DocFile) error {
	docPath, summary, sections := f.Path, f.Summary, f.Sections
	return d.s.InTx(ctx, func(q *gen.Queries, _ pgx.Tx) error {
		parent := NodeID(repoID, "", "")
		if err := q.UpsertDocNode(ctx, gen.UpsertDocNodeParams{ID: parent, RepoID: repoID, Kind: gen.DocNodeKindRepo, Path: "", Title: repoName}); err != nil {
			return err
		}
		dir := ""
		for _, part := range strings.Split(path.Dir(docPath), "/") {
			if part == "." || part == "" {
				continue
			}
			dir = path.Join(dir, part)
			id := NodeID(repoID, dir+"/", "")
			if err := q.UpsertDocNode(ctx, gen.UpsertDocNodeParams{ID: id, RepoID: repoID, ParentID: &parent, Kind: gen.DocNodeKindDir,
				Path: dir + "/", Title: part, OrderKey: part}); err != nil {
				return err
			}
			parent = id
		}
		fileID := NodeID(repoID, docPath, "")
		title := strings.TrimSuffix(path.Base(docPath), ".md")
		if err := q.UpsertDocNode(ctx, gen.UpsertDocNodeParams{ID: fileID, RepoID: repoID, ParentID: &parent, Kind: gen.DocNodeKindFile,
			Path: docPath, Title: title, Summary: summary, OrderKey: title, Content: f.Content, CommitSha: f.CommitSHA}); err != nil {
			return err
		}
		if err := q.DeleteFileSections(ctx, gen.DeleteFileSectionsParams{RepoID: repoID, Path: docPath}); err != nil {
			return err
		}
		for i, s := range sections {
			cid := s.ChunkID
			if err := q.UpsertDocNode(ctx, gen.UpsertDocNodeParams{ID: NodeID(repoID, docPath, s.ChunkID), RepoID: repoID, ParentID: &fileID,
				Kind: gen.DocNodeKindSection, Path: docPath, Title: s.Title, ChunkID: &cid, OrderKey: fmt.Sprintf("%05d", i)}); err != nil {
				return err
			}
		}
		if d.shelves == nil {
			return nil
		}
		if err := q.ClearAutoShelfItems(ctx, gen.ClearAutoShelfItemsParams{ItemType: gen.ShelfItemTypeDocNode, ItemID: fileID}); err != nil {
			return err
		}
		for _, slug := range d.shelves.Assign(library.Item{Type: library.ItemDocNode, ID: fileID, Title: title, Path: docPath, Source: string(ports.SourceGeneratedDoc)}) {
			if err := q.AddShelfItem(ctx, gen.AddShelfItemParams{ItemType: gen.ShelfItemTypeDocNode, ItemID: fileID, Slug: slug}); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveFile drops a doc file and its sections from the Tree.
func (d *Docs) RemoveFile(ctx context.Context, repoID, docPath string) error {
	return d.s.Q.DeleteDocFile(ctx, gen.DeleteDocFileParams{RepoID: repoID, Path: docPath})
}

// SeedShelves inserts the classifier's shelves that do not exist yet (operators' edits are kept).
func (d *Docs) SeedShelves(ctx context.Context) error {
	if d.shelves == nil {
		return nil
	}
	for _, sh := range d.shelves.Shelves() {
		rule, _ := json.Marshal(sh.Rules)
		if _, err := d.s.Q.UpsertShelf(ctx, gen.UpsertShelfParams{ID: ports.NewID(), Slug: sh.Slug, Title: sh.Title,
			Description: sh.Description, Rule: rule, Curated: sh.Curated, OrderKey: int32(sh.Order)}); err != nil && !IsNoRows(err) {
			return fmt.Errorf("seed shelf %s: %w", sh.Slug, err)
		}
	}
	return nil
}

// ShelfItemIDs lists item IDs on a shelf.
func (d *Docs) ShelfItemIDs(ctx context.Context, slug string) ([]string, error) {
	rows, err := d.s.Q.ShelfItems(ctx, slug)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ItemID
	}
	return out, nil
}

// Children lists Tree nodes under parent ("" = the roots).
func (d *Docs) Children(ctx context.Context, repoID, parentID string) ([]gen.DocNode, error) {
	return d.s.Q.DocChildren(ctx, gen.DocChildrenParams{RepoID: repoID, ParentID: strPtr(parentID)})
}

// RecordRename audits a rename re-key.
func (c *Chunks) RecordRename(ctx context.Context, repoID, oldPath, newPath, sha string) error {
	return c.s.Q.InsertRename(ctx, gen.InsertRenameParams{RepoID: repoID, OldPath: oldPath, NewPath: newPath, CommitSha: sha})
}

// GC hard-deletes chunks soft-deleted before cutoff and returns their IDs (so vector stores without
// cascading deletes can drop them too).
func (c *Chunks) GC(ctx context.Context, cutoff time.Time) ([]string, error) {
	return c.s.Q.GCChunks(ctx, &cutoff)
}

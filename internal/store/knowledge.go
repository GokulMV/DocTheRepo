package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/manifest"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Knowledge stores synced Confluence pages and Jira issues: their chunks (repo-less, readable by every
// viewer), Palace entities and mention links, Library placements, and the knowledge_docs index.
type Knowledge struct {
	s       *Store
	shelves *library.Classifier
}

// NewKnowledge returns the knowledge store; shelves places documents on Library shelves (nil disables it).
func NewKnowledge(s *Store, shelves *library.Classifier) *Knowledge {
	return &Knowledge{s: s, shelves: shelves}
}

// KnowledgePath is a document's chunk path: <source>/<space>/<external id> (stable across renames).
func KnowledgePath(d ports.KnowledgeDoc) string {
	return string(d.Source) + "/" + d.Space + "/" + d.ExternalID
}

// KnowledgeApplied summarises one Apply.
type KnowledgeApplied = ports.KnowledgeApplied

func entityKind(src ports.ChunkSource) string {
	if src == ports.SourceJira {
		return palace.KindJiraIssue
	}
	return palace.KindConfluencePage
}

func shelfType(src ports.ChunkSource) gen.ShelfItemType {
	if src == ports.SourceJira {
		return gen.ShelfItemTypeJiraIssue
	}
	return gen.ShelfItemTypeConfluencePage
}

// MentionIndex loads the entities documents can link to by name.
func (k *Knowledge) MentionIndex(ctx context.Context) (*palace.MentionIndex, error) {
	rows, err := k.s.Q.MentionableEntities(ctx)
	if err != nil {
		return nil, err
	}
	es := make([]palace.Entity, len(rows))
	for i, r := range rows {
		es[i] = palace.Entity{Ref: palace.Ref{Kind: r.Kind, Key: r.Key}, Name: r.Name}
	}
	return palace.NewMentionIndex(es), nil
}

// isRunbook marks documents that are operational procedures: their mention links are runbook_for.
func isRunbook(d ports.KnowledgeDoc) bool {
	for _, l := range d.Labels {
		switch l {
		case "runbook", "playbook", "oncall", "on-call":
			return true
		}
	}
	t := strings.ToLower(d.Title)
	return strings.Contains(t, "runbook") || strings.Contains(t, "playbook")
}

// Apply writes documents: chunks via the manifest diff (unchanged content is not re-embedded), the
// knowledge_docs row, the Palace entity with documented_in/runbook_for links to mentioned services,
// repositories, and endpoints, and Library placements. Each document is one transaction.
func (k *Knowledge) Apply(ctx context.Context, connectorID string, docs []ports.KnowledgeDoc, mentions *palace.MentionIndex) (KnowledgeApplied, error) {
	var res KnowledgeApplied
	for _, d := range docs {
		if d.ExternalID == "" || d.Space == "" {
			continue
		}
		p := KnowledgePath(d)
		md := strings.TrimSpace(d.Markdown)
		if !strings.HasPrefix(md, "# ") {
			md = "# " + d.Title + "\n\n" + md
		}
		fresh := chunker.Docs(string(d.Source)+":"+d.Space, "", p, []byte(md), chunker.DocOptions{Source: d.Source, URL: d.URL})
		sum := sha256.Sum256([]byte(md))
		hash := hex.EncodeToString(sum[:])
		refs := mentions.Mentions(d.Title + "\n" + md)
		err := k.s.InTx(ctx, func(q *gen.Queries, tx pgx.Tx) error {
			rows, err := q.SharedChunksForPath(ctx, gen.SharedChunksForPathParams{Source: gen.ChunkSource(d.Source), Path: p})
			if err != nil {
				return err
			}
			delta := manifest.Diff(toChunks(rows), fresh, []string{p})
			a, c, r := delta.Counts()
			res.Added, res.Changed, res.Removed = res.Added+a, res.Changed+c, res.Removed+r
			if a+c+r == 0 && len(delta.Revived) == 0 {
				res.Unchanged++
			}
			w := ports.ChunkWrite{Upserts: append(append([]ports.Chunk{}, delta.Added...), delta.Changed...), Revive: delta.Revived, Remove: delta.Removed}
			if err := applyChunkWrite(ctx, q, w); err != nil {
				return err
			}
			res.Embed = append(res.Embed, delta.NeedsEmbedding()...)

			var docID string
			var updated *time.Time
			if !d.UpdatedAt.IsZero() {
				updated = &d.UpdatedAt
			}
			labels := d.Labels
			if labels == nil {
				labels = []string{}
			}
			err = tx.QueryRow(ctx, `INSERT INTO knowledge_docs (id, connector_id, source, external_id, space, title, url, labels, status, done,
					path, summary, content_hash, upstream_updated_at, synced_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now())
				ON CONFLICT (source, external_id) DO UPDATE SET connector_id = EXCLUDED.connector_id, space = EXCLUDED.space,
					title = EXCLUDED.title, url = EXCLUDED.url, labels = EXCLUDED.labels, status = EXCLUDED.status, done = EXCLUDED.done,
					path = EXCLUDED.path, summary = EXCLUDED.summary, content_hash = EXCLUDED.content_hash,
					upstream_updated_at = EXCLUDED.upstream_updated_at, synced_at = now()
				RETURNING id`,
				ports.NewID(), connectorID, string(d.Source), d.ExternalID, d.Space, d.Title, d.URL, labels, d.Status, d.Done,
				p, summarize(md), hash, updated).Scan(&docID)
			if err != nil {
				return fmt.Errorf("upsert knowledge doc %s: %w", d.ExternalID, err)
			}

			attrs, _ := json.Marshal(map[string]string{"url": d.URL, "space": d.Space, "status": d.Status, "source": string(d.Source)})
			pageID, err := q.UpsertEntity(ctx, gen.UpsertEntityParams{ID: ports.NewID(), Kind: entityKind(d.Source), Key: d.ExternalID,
				Name: d.Title, Attrs: attrs})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE entities SET name = $1 WHERE id = $2 AND name <> $1`, d.Title, pageID); err != nil {
				return err
			}
			if err := q.RetireSharedSourceEdges(ctx, p); err != nil {
				return err
			}
			runbook := isRunbook(d)
			for _, ref := range refs {
				id, err := q.LiveEntityID(ctx, gen.LiveEntityIDParams{Kind: ref.Kind, Key: ref.Key})
				if IsNoRows(err) {
					continue
				}
				if err != nil {
					return err
				}
				ev, _ := json.Marshal(palace.Evidence{Path: d.URL})
				if err := q.UpsertEdge(ctx, gen.UpsertEdgeParams{SrcID: id, Kind: palace.EdgeDocumentedIn, DstID: pageID, Evidence: ev, SourcePath: p}); err != nil {
					return err
				}
				if runbook {
					if err := q.UpsertEdge(ctx, gen.UpsertEdgeParams{SrcID: pageID, Kind: palace.EdgeRunbookFor, DstID: id, Evidence: ev, SourcePath: p}); err != nil {
						return err
					}
				}
				res.Links++
			}

			if k.shelves != nil {
				st := shelfType(d.Source)
				if err := q.ClearAutoShelfItems(ctx, gen.ClearAutoShelfItemsParams{ItemType: st, ItemID: docID}); err != nil {
					return err
				}
				for _, slug := range k.shelves.Assign(library.Item{Type: string(st), ID: docID, Title: d.Title, Source: string(d.Source), Labels: d.Labels}) {
					if err := q.AddShelfItem(ctx, gen.AddShelfItemParams{ItemType: st, ItemID: docID, Slug: slug}); err != nil {
						return err
					}
				}
			}
			if !w.Empty() {
				_, err = q.BumpIndexVersion(ctx)
			}
			return err
		})
		if err != nil {
			return res, err
		}
		res.Docs++
	}
	return res, nil
}

// applyChunkWrite applies a manifest update inside a transaction.
func applyChunkWrite(ctx context.Context, q *gen.Queries, w ports.ChunkWrite) error {
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
	return nil
}

// summarize is a document's one-paragraph summary for the Library.
func summarize(md string) string {
	for _, para := range strings.Split(md, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" || strings.HasPrefix(para, "#") || strings.HasPrefix(para, "**Type:**") || strings.HasPrefix(para, "```") {
			continue
		}
		para = strings.Join(strings.Fields(para), " ")
		if len(para) > 300 {
			para = para[:297] + "..."
		}
		return para
	}
	return ""
}

// RemoveMissing deletes the documents of one connector stream that are no longer upstream (deleted or
// moved out of the synced space): their chunks are soft-deleted, links retired, entity retired, Library
// placements dropped. An empty live list is ignored, so an upstream glitch cannot wipe a space.
func (k *Knowledge) RemoveMissing(ctx context.Context, connectorID, space string, live []string) (int, error) {
	if len(live) == 0 {
		return 0, nil
	}
	rows, err := k.s.Pool.Query(ctx, `SELECT id, source::text, external_id, path FROM knowledge_docs
		WHERE connector_id = $1 AND space = $2 AND NOT (external_id = ANY($3::text[]))`, connectorID, space, live)
	if err != nil {
		return 0, err
	}
	type gone struct{ id, source, ext, path string }
	var gs []gone
	for rows.Next() {
		var g gone
		if err := rows.Scan(&g.id, &g.source, &g.ext, &g.path); err != nil {
			rows.Close()
			return 0, err
		}
		gs = append(gs, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, g := range gs {
		src := ports.ChunkSource(g.source)
		err := k.s.InTx(ctx, func(q *gen.Queries, tx pgx.Tx) error {
			if _, err := q.SoftDeleteSharedPath(ctx, gen.SoftDeleteSharedPathParams{Source: gen.ChunkSource(src), Path: g.path}); err != nil {
				return err
			}
			if err := q.RetireSharedSourceEdges(ctx, g.path); err != nil {
				return err
			}
			if err := q.RetireEntity(ctx, gen.RetireEntityParams{Kind: entityKind(src), Key: g.ext}); err != nil {
				return err
			}
			if err := q.DeleteShelfItemsFor(ctx, gen.DeleteShelfItemsForParams{ItemType: shelfType(src), ItemID: g.id}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM knowledge_docs WHERE id = $1`, g.id); err != nil {
				return err
			}
			_, err := q.BumpIndexVersion(ctx)
			return err
		})
		if err != nil {
			return 0, err
		}
	}
	return len(gs), nil
}

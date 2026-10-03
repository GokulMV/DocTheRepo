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
	switch src {
	case ports.SourceJira:
		return palace.KindJiraIssue
	case ports.SourceNotion:
		return palace.KindNotionPage
	case ports.SourceUpload:
		return palace.KindDocument
	}
	return palace.KindConfluencePage
}

func shelfType(src ports.ChunkSource) gen.ShelfItemType {
	switch src {
	case ports.SourceJira:
		return gen.ShelfItemTypeJiraIssue
	case ports.SourceNotion, ports.SourceUpload:
		return gen.ShelfItemType("knowledge_doc")
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
			body := "" // only uploads are shown by the Hub; synced pages link to their own site
			if d.Source == ports.SourceUpload {
				body = md
			}
			err = tx.QueryRow(ctx, `INSERT INTO knowledge_docs (id, connector_id, source, external_id, space, title, url, labels, status, done,
					path, summary, content_hash, upstream_updated_at, synced_at, body, uploaded_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now(), $15, $16)
				ON CONFLICT (source, external_id) DO UPDATE SET connector_id = EXCLUDED.connector_id, space = EXCLUDED.space,
					title = EXCLUDED.title, url = EXCLUDED.url, labels = EXCLUDED.labels, status = EXCLUDED.status, done = EXCLUDED.done,
					path = EXCLUDED.path, summary = EXCLUDED.summary, content_hash = EXCLUDED.content_hash,
					upstream_updated_at = EXCLUDED.upstream_updated_at, synced_at = now(), body = EXCLUDED.body,
					uploaded_by = coalesce(EXCLUDED.uploaded_by, knowledge_docs.uploaded_by)
				RETURNING id`,
				ports.NewID(), connectorID, string(d.Source), d.ExternalID, d.Space, d.Title, d.URL, labels, d.Status, d.Done,
				p, summarize(md), hash, updated, body, strPtr(d.UploadedBy)).Scan(&docID)
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
		if err := k.removeDoc(ctx, g.id, ports.ChunkSource(g.source), g.ext, g.path); err != nil {
			return 0, err
		}
	}
	return len(gs), nil
}

// removeDoc deletes one document: chunks soft-deleted, links and entity retired, Library placements and
// the row dropped.
func (k *Knowledge) removeDoc(ctx context.Context, id string, src ports.ChunkSource, ext, path string) error {
	return k.s.InTx(ctx, func(q *gen.Queries, tx pgx.Tx) error {
		if _, err := q.SoftDeleteSharedPath(ctx, gen.SoftDeleteSharedPathParams{Source: gen.ChunkSource(src), Path: path}); err != nil {
			return err
		}
		if err := q.RetireSharedSourceEdges(ctx, path); err != nil {
			return err
		}
		if err := q.RetireEntity(ctx, gen.RetireEntityParams{Kind: entityKind(src), Key: ext}); err != nil {
			return err
		}
		if err := q.DeleteShelfItemsFor(ctx, gen.DeleteShelfItemsForParams{ItemType: shelfType(src), ItemID: id}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM knowledge_docs WHERE id = $1`, id); err != nil {
			return err
		}
		_, err := q.BumpIndexVersion(ctx)
		return err
	})
}

// UploadConnectorName names the internal connector that holds uploaded documents.
const UploadConnectorName = "Uploaded documents"

// UploadConnector returns the internal connector uploaded documents belong to, creating it on first use.
func (k *Knowledge) UploadConnector(ctx context.Context) (string, error) {
	var id string
	err := k.s.Pool.QueryRow(ctx, `SELECT id FROM connectors WHERE type = 'upload' ORDER BY created_at LIMIT 1`).Scan(&id)
	if err == nil || !IsNoRows(err) {
		return id, err
	}
	err = k.s.Pool.QueryRow(ctx, `INSERT INTO connectors (id, type, name, mode) VALUES ($1, 'upload', $2, 'poll')
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id`, ports.NewID(), UploadConnectorName).Scan(&id)
	return id, err
}

// Upload is an uploaded document as the Library lists it (Body only when one is read).
type Upload struct {
	ID         string    `json:"id"` // the external ID, stable for a collection + file name
	Collection string    `json:"collection"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	UploadedBy string    `json:"uploaded_by,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	Body       string    `json:"body,omitempty"`
}

// Uploads lists uploaded documents, newest first.
func (k *Knowledge) Uploads(ctx context.Context) ([]Upload, error) {
	rows, err := k.s.Pool.Query(ctx, `SELECT k.external_id, k.space, k.title, k.summary, coalesce(u.email, ''), k.synced_at
		FROM knowledge_docs k LEFT JOIN users u ON u.id = k.uploaded_by
		WHERE k.source = 'upload' ORDER BY k.synced_at DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upload{}
	for rows.Next() {
		var u Upload
		if err := rows.Scan(&u.ID, &u.Collection, &u.Title, &u.Summary, &u.UploadedBy, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetUpload reads one uploaded document with its Markdown.
func (k *Knowledge) GetUpload(ctx context.Context, id string) (Upload, error) {
	var u Upload
	err := k.s.Pool.QueryRow(ctx, `SELECT k.external_id, k.space, k.title, k.summary, coalesce(u.email, ''), k.synced_at, k.body
		FROM knowledge_docs k LEFT JOIN users u ON u.id = k.uploaded_by
		WHERE k.source = 'upload' AND k.external_id = $1`, id).Scan(&u.ID, &u.Collection, &u.Title, &u.Summary, &u.UploadedBy, &u.UpdatedAt, &u.Body)
	if IsNoRows(err) {
		return u, ports.ErrNotFound
	}
	return u, err
}

// DeleteUpload removes an uploaded document from search and the Library.
func (k *Knowledge) DeleteUpload(ctx context.Context, id string) error {
	var docID, path string
	err := k.s.Pool.QueryRow(ctx, `SELECT id, path FROM knowledge_docs WHERE source = 'upload' AND external_id = $1`, id).Scan(&docID, &path)
	if IsNoRows(err) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	return k.removeDoc(ctx, docID, ports.SourceUpload, id, path)
}

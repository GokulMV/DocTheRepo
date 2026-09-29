-- name: UpsertEntity :one
INSERT INTO entities (id, kind, key, name, repo_id, attrs)
VALUES (sqlc.arg(id), sqlc.arg(kind), sqlc.arg(key), sqlc.arg(name), sqlc.narg(repo_id), sqlc.arg(attrs))
ON CONFLICT (kind, key) DO UPDATE SET attrs = entities.attrs || EXCLUDED.attrs, last_seen = now(), deleted_at = NULL,
    repo_id = coalesce(entities.repo_id, EXCLUDED.repo_id)
RETURNING id;

-- name: RetireSourceEdges :exec
-- Marks every edge a file contributed as deleted; the re-extraction below revives what still exists.
UPDATE edges SET deleted_at = now() WHERE repo_id = sqlc.arg(repo_id) AND source_path = sqlc.arg(source_path) AND deleted_at IS NULL;

-- name: UpsertEdge :exec
INSERT INTO edges (src_id, kind, dst_id, evidence, repo_id, source_path)
VALUES (sqlc.arg(src_id), sqlc.arg(kind), sqlc.arg(dst_id), sqlc.arg(evidence), sqlc.narg(repo_id), sqlc.arg(source_path))
ON CONFLICT (src_id, kind, dst_id, source_path) DO UPDATE SET evidence = EXCLUDED.evidence, last_seen = now(), deleted_at = NULL;

-- name: RetireOrphanEntities :execrows
-- Entities of a repo that no live edge references any more (a deleted symbol, endpoint, file).
UPDATE entities e SET deleted_at = now()
WHERE e.repo_id = sqlc.arg(repo_id) AND e.deleted_at IS NULL AND e.kind NOT IN ('repo', 'service')
  AND NOT EXISTS (SELECT 1 FROM edges x WHERE (x.src_id = e.id OR x.dst_id = e.id) AND x.deleted_at IS NULL);

-- name: EntityByRef :one
SELECT * FROM entities WHERE kind = sqlc.arg(kind) AND key = sqlc.arg(key);

-- name: EdgesFrom :many
SELECT x.kind, s.kind AS src_kind, s.key AS src_key, d.kind AS dst_kind, d.key AS dst_key, x.evidence
FROM edges x JOIN entities s ON s.id = x.src_id JOIN entities d ON d.id = x.dst_id
WHERE x.src_id = sqlc.arg(id) AND x.deleted_at IS NULL ORDER BY x.kind, d.key;

-- name: UpsertDocNode :exec
INSERT INTO doc_nodes (id, repo_id, parent_id, kind, path, title, summary, chunk_id, order_key, content, commit_sha, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(repo_id), sqlc.narg(parent_id), sqlc.arg(kind), sqlc.arg(path), sqlc.arg(title),
        sqlc.arg(summary), sqlc.narg(chunk_id), sqlc.arg(order_key), sqlc.arg(content), sqlc.arg(commit_sha), now())
ON CONFLICT (id) DO UPDATE SET parent_id = EXCLUDED.parent_id, title = EXCLUDED.title, summary = EXCLUDED.summary,
    chunk_id = EXCLUDED.chunk_id, order_key = EXCLUDED.order_key, content = EXCLUDED.content, commit_sha = EXCLUDED.commit_sha,
    updated_at = now();

-- name: DeleteFileSections :exec
DELETE FROM doc_nodes WHERE repo_id = sqlc.arg(repo_id) AND path = sqlc.arg(path) AND kind = 'section';

-- name: DeleteDocFile :exec
DELETE FROM doc_nodes WHERE repo_id = sqlc.arg(repo_id) AND path = sqlc.arg(path);

-- name: DocChildren :many
SELECT * FROM doc_nodes WHERE repo_id = sqlc.arg(repo_id) AND parent_id IS NOT DISTINCT FROM sqlc.narg(parent_id)
ORDER BY kind, order_key, title;

-- name: UpsertShelf :one
INSERT INTO library_shelves (id, slug, title, description, rule, curated, order_key)
VALUES (sqlc.arg(id), sqlc.arg(slug), sqlc.arg(title), sqlc.arg(description), sqlc.arg(rule), sqlc.arg(curated), sqlc.arg(order_key))
ON CONFLICT (slug) DO NOTHING
RETURNING id;

-- name: ListShelves :many
SELECT * FROM library_shelves ORDER BY order_key, slug;

-- name: ClearAutoShelfItems :exec
-- Removes rule-assigned (unpinned) placements of an item on non-curated shelves before re-assignment.
DELETE FROM shelf_items si USING library_shelves s
WHERE si.shelf_id = s.id AND NOT s.curated AND NOT si.pinned AND si.item_type = sqlc.arg(item_type) AND si.item_id = sqlc.arg(item_id);

-- name: AddShelfItem :exec
INSERT INTO shelf_items (shelf_id, item_type, item_id)
SELECT id, sqlc.arg(item_type), sqlc.arg(item_id) FROM library_shelves WHERE slug = sqlc.arg(slug)
ON CONFLICT DO NOTHING;

-- name: ShelfItems :many
SELECT si.* FROM shelf_items si JOIN library_shelves s ON s.id = si.shelf_id WHERE s.slug = sqlc.arg(slug) ORDER BY si.item_id;

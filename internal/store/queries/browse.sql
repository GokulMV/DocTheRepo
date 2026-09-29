-- name: GetDocNode :one
SELECT n.*, r.full_name AS repo_name FROM doc_nodes n JOIN repos r ON r.id = n.repo_id WHERE n.id = sqlc.arg(id);

-- name: DocChildrenWithCounts :many
SELECT n.id, n.kind, n.title, n.path, n.summary, n.chunk_id, n.updated_at,
       EXISTS (SELECT 1 FROM doc_nodes c WHERE c.parent_id = n.id) AS has_children
FROM doc_nodes n
WHERE n.repo_id = sqlc.arg(repo_id) AND n.parent_id IS NOT DISTINCT FROM sqlc.narg(parent_id)
ORDER BY CASE n.kind WHEN 'dir' THEN 0 WHEN 'file' THEN 1 ELSE 2 END, n.order_key, n.title;

-- name: DocRoots :many
SELECT n.id, n.repo_id, n.title, EXISTS (SELECT 1 FROM doc_nodes c WHERE c.parent_id = n.id) AS has_children
FROM doc_nodes n
WHERE n.kind = 'repo' AND (sqlc.arg(all_repos)::boolean OR n.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
ORDER BY n.title;

-- name: DocSectionChunks :many
SELECT c.chunk_id, c.path, c.symbol, c.language, c.signature, c.commit_sha
FROM doc_nodes n JOIN chunks c ON c.chunk_id = n.chunk_id AND c.deleted_at IS NULL
WHERE n.parent_id = sqlc.arg(file_id) AND n.kind = 'section' ORDER BY n.order_key;

-- name: ListEntities :many
SELECT e.id, e.kind, e.key, e.name, e.repo_id, e.attrs, e.last_seen
FROM entities e
WHERE e.deleted_at IS NULL
  AND (sqlc.arg(kind)::text = '' OR e.kind = sqlc.arg(kind))
  AND (sqlc.arg(q)::text = '' OR e.name ILIKE '%' || sqlc.arg(q) || '%' OR e.key ILIKE '%' || sqlc.arg(q) || '%')
  AND (sqlc.narg(repo_id)::uuid IS NULL OR e.repo_id = sqlc.narg(repo_id))
  AND (e.repo_id IS NULL OR sqlc.arg(all_repos)::boolean OR e.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
  AND (e.kind, e.key) > (sqlc.arg(after_kind)::text, sqlc.arg(after_key)::text)
ORDER BY e.kind, e.key
LIMIT sqlc.arg(lim);

-- name: GetEntity :one
SELECT id, kind, key, name, repo_id, attrs, last_seen FROM entities WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: EntityEdges :many
-- Live edges touching the given entities, with both endpoints, ACL-filtered on repo-owned endpoints.
SELECT x.src_id, x.kind, x.dst_id, x.evidence,
       s.kind AS src_kind, s.key AS src_key, s.name AS src_name, s.repo_id AS src_repo,
       d.kind AS dst_kind, d.key AS dst_key, d.name AS dst_name, d.repo_id AS dst_repo
FROM edges x
JOIN entities s ON s.id = x.src_id AND s.deleted_at IS NULL
JOIN entities d ON d.id = x.dst_id AND d.deleted_at IS NULL
WHERE x.deleted_at IS NULL
  AND (x.src_id = ANY(sqlc.arg(ids)::uuid[]) OR x.dst_id = ANY(sqlc.arg(ids)::uuid[]))
  AND (cardinality(sqlc.arg(kinds)::text[]) = 0 OR x.kind = ANY(sqlc.arg(kinds)::text[]))
  AND (s.repo_id IS NULL OR sqlc.arg(all_repos)::boolean OR s.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
  AND (d.repo_id IS NULL OR sqlc.arg(all_repos)::boolean OR d.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
LIMIT 2000;

-- name: ShelvesWithCounts :many
SELECT s.id, s.slug, s.title, s.description, s.rule, s.curated, s.order_key, count(si.item_id) AS items
FROM library_shelves s LEFT JOIN shelf_items si ON si.shelf_id = s.id
GROUP BY s.id ORDER BY s.order_key, s.slug;

-- name: GetShelfBySlug :one
SELECT * FROM library_shelves WHERE slug = sqlc.arg(slug);

-- name: ShelfDocItems :many
SELECT si.item_type, si.item_id, si.pinned, si.note, n.title, n.path, n.summary, n.repo_id
FROM shelf_items si JOIN doc_nodes n ON n.id = si.item_id
WHERE si.shelf_id = sqlc.arg(shelf_id) AND si.item_type = 'doc_node'
  AND (sqlc.arg(all_repos)::boolean OR n.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
ORDER BY si.pinned DESC, n.title;

-- name: ShelfEntityItems :many
SELECT si.item_type, si.item_id, si.pinned, si.note, e.name AS title, e.key AS path, e.kind AS summary, e.repo_id
FROM shelf_items si JOIN entities e ON e.id::text = si.item_id
WHERE si.shelf_id = sqlc.arg(shelf_id) AND si.item_type = 'entity'
  AND (e.repo_id IS NULL OR sqlc.arg(all_repos)::boolean OR e.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
ORDER BY si.pinned DESC, e.name;

-- name: CreateShelf :exec
INSERT INTO library_shelves (id, slug, title, description, rule, curated, order_key)
VALUES (sqlc.arg(id), sqlc.arg(slug), sqlc.arg(title), sqlc.arg(description), sqlc.arg(rule), sqlc.arg(curated), sqlc.arg(order_key));

-- name: UpdateShelf :execrows
UPDATE library_shelves SET title = coalesce(sqlc.narg(title), title), description = coalesce(sqlc.narg(description), description),
    order_key = coalesce(sqlc.narg(order_key), order_key)
WHERE id = sqlc.arg(id);

-- name: DeleteShelf :execrows
DELETE FROM library_shelves WHERE id = sqlc.arg(id);

-- name: PinShelfItem :execrows
INSERT INTO shelf_items (shelf_id, item_type, item_id, pinned, note) VALUES (sqlc.arg(shelf_id), sqlc.arg(item_type), sqlc.arg(item_id), true, sqlc.arg(note))
ON CONFLICT (shelf_id, item_type, item_id) DO UPDATE SET pinned = true, note = EXCLUDED.note;

-- name: UpdateRepoSettings :execrows
UPDATE repos SET tracked_branch = coalesce(sqlc.narg(tracked_branch), tracked_branch),
    docs_path = coalesce(sqlc.narg(docs_path), docs_path),
    push_mode = coalesce(sqlc.narg(push_mode)::push_mode, push_mode),
    on_reject = coalesce(sqlc.narg(on_reject), on_reject),
    approver = coalesce(sqlc.narg(approver), approver),
    service_name = coalesce(sqlc.narg(service_name), service_name),
    owners = coalesce(sqlc.narg(owners)::text[], owners),
    enabled = coalesce(sqlc.narg(enabled), enabled),
    pr_conflict_strategy = coalesce(sqlc.narg(pr_conflict_strategy), pr_conflict_strategy),
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: ListConnectorsPublic :many
SELECT id, type, name, config, mode, extract(epoch FROM poll_interval)::bigint AS poll_seconds, enabled, health, last_error, last_sync_at,
       creds_ciphertext IS NOT NULL AS has_credentials, webhook_secret_ct IS NOT NULL AS has_webhook_secret, created_at
FROM connectors ORDER BY name;

-- name: UpdateConnector :execrows
UPDATE connectors SET name = coalesce(sqlc.narg(name), name), config = coalesce(sqlc.narg(config), config),
    creds_ciphertext = coalesce(sqlc.narg(creds_ciphertext), creds_ciphertext),
    webhook_secret_ct = coalesce(sqlc.narg(webhook_secret_ct), webhook_secret_ct),
    webhook_secret_hash = coalesce(sqlc.narg(webhook_secret_hash), webhook_secret_hash),
    mode = coalesce(sqlc.narg(mode)::connector_mode, mode),
    poll_interval = coalesce(make_interval(secs => sqlc.narg(poll_seconds)::float8), poll_interval),
    enabled = coalesce(sqlc.narg(enabled), enabled), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: DeleteConnector :execrows
DELETE FROM connectors WHERE id = sqlc.arg(id);

-- name: ReplaceSpendLimitsDelete :exec
DELETE FROM spend_limits;

-- name: InsertSpendLimit :exec
INSERT INTO spend_limits (id, scope, scope_key, time_window, max_tokens, max_cost_usd, on_breach, alert_url)
VALUES (sqlc.arg(id), sqlc.arg(scope), sqlc.arg(scope_key), sqlc.arg(time_window), sqlc.narg(max_tokens), sqlc.narg(max_cost_usd),
        sqlc.arg(on_breach), sqlc.arg(alert_url));

-- name: CountLiveChunks :one
SELECT count(*) FROM chunks WHERE deleted_at IS NULL;

-- name: LiveChunkTokens :one
SELECT coalesce(sum(length(content)), 0)::bigint / 4 AS tokens FROM chunks WHERE deleted_at IS NULL;

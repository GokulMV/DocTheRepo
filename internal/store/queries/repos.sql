-- name: CreateConnector :one
INSERT INTO connectors (id, type, name, config, creds_ciphertext, webhook_secret_hash, webhook_secret_ct, mode, poll_interval)
VALUES (sqlc.arg(id), sqlc.arg(type), sqlc.arg(name), sqlc.arg(config), sqlc.narg(creds_ciphertext),
        sqlc.narg(webhook_secret_hash), sqlc.narg(webhook_secret_ct), sqlc.arg(mode),
        make_interval(secs => sqlc.arg(poll_seconds)::float8))
RETURNING id;

-- name: GetConnector :one
SELECT id, type, name, config, creds_ciphertext, webhook_secret_ct, mode,
       extract(epoch FROM poll_interval)::bigint AS poll_seconds, enabled, health, last_error, last_sync_at
FROM connectors WHERE id = sqlc.arg(id);

-- name: ListConnectorsByType :many
SELECT id, type, name, config, creds_ciphertext, webhook_secret_ct, mode,
       extract(epoch FROM poll_interval)::bigint AS poll_seconds, enabled, health, last_error, last_sync_at
FROM connectors WHERE type::text = ANY(sqlc.arg(types)::text[]) AND enabled ORDER BY name;

-- name: SetConnectorHealth :exec
UPDATE connectors SET health = sqlc.arg(health), last_error = sqlc.arg(last_error),
    last_sync_at = CASE WHEN sqlc.arg(synced)::boolean THEN now() ELSE last_sync_at END, updated_at = now()
WHERE id = sqlc.arg(id);

-- name: UpsertRepo :one
INSERT INTO repos (id, connector_id, full_name, default_branch, tracked_branch, docs_path, push_mode, on_reject, approver,
                   service_name, pr_conflict_strategy, pr_stale_after)
VALUES (sqlc.arg(id), sqlc.arg(connector_id), sqlc.arg(full_name), sqlc.arg(default_branch), sqlc.arg(tracked_branch),
        sqlc.arg(docs_path), sqlc.arg(push_mode), sqlc.arg(on_reject), sqlc.arg(approver), sqlc.arg(service_name),
        sqlc.arg(pr_conflict_strategy), make_interval(secs => sqlc.arg(stale_seconds)::float8))
ON CONFLICT (connector_id, full_name) DO UPDATE SET default_branch = EXCLUDED.default_branch,
    tracked_branch = EXCLUDED.tracked_branch, docs_path = EXCLUDED.docs_path, push_mode = EXCLUDED.push_mode,
    on_reject = EXCLUDED.on_reject, approver = EXCLUDED.approver, service_name = EXCLUDED.service_name,
    pr_conflict_strategy = EXCLUDED.pr_conflict_strategy, pr_stale_after = EXCLUDED.pr_stale_after, updated_at = now()
RETURNING id;

-- name: GetRepo :one
SELECT r.*, extract(epoch FROM r.pr_stale_after)::bigint AS stale_seconds, c.type AS connector_type
FROM repos r JOIN connectors c ON c.id = r.connector_id WHERE r.id = sqlc.arg(id);

-- name: GetRepoByName :one
SELECT r.*, extract(epoch FROM r.pr_stale_after)::bigint AS stale_seconds, c.type AS connector_type
FROM repos r JOIN connectors c ON c.id = r.connector_id
WHERE r.connector_id = sqlc.arg(connector_id) AND r.full_name = sqlc.arg(full_name);

-- name: ListReposByConnector :many
SELECT r.*, extract(epoch FROM r.pr_stale_after)::bigint AS stale_seconds, c.type AS connector_type
FROM repos r JOIN connectors c ON c.id = r.connector_id
WHERE r.connector_id = sqlc.arg(connector_id) ORDER BY r.full_name;

-- name: ListEnabledRepos :many
SELECT r.*, extract(epoch FROM r.pr_stale_after)::bigint AS stale_seconds, c.type AS connector_type
FROM repos r JOIN connectors c ON c.id = r.connector_id
WHERE r.enabled AND c.enabled ORDER BY r.full_name;

-- name: SetLastProcessedSHA :exec
UPDATE repos SET last_processed_sha = sqlc.arg(sha), updated_at = now() WHERE id = sqlc.arg(id);

-- name: InsertSavings :exec
INSERT INTO savings_events (id, kind, est_tokens_avoided, est_cost_avoided_usd, ref_id)
VALUES (sqlc.arg(id), sqlc.arg(kind), sqlc.arg(est_tokens_avoided), sqlc.arg(est_cost_avoided_usd), sqlc.arg(ref_id));

-- name: InsertRename :exec
INSERT INTO manifest_renames (repo_id, old_path, new_path, commit_sha) VALUES (sqlc.arg(repo_id), sqlc.arg(old_path), sqlc.arg(new_path), sqlc.arg(commit_sha));

-- name: SavePR :exec
INSERT INTO prs (repo_id, number, url, branch, base, job_id, mode, state, chunk_ids, source_paths, approver, note, opened_at, updated_at)
VALUES (sqlc.arg(repo_id), sqlc.arg(number), sqlc.arg(url), sqlc.arg(branch), sqlc.arg(base), sqlc.narg(job_id), sqlc.arg(mode),
        sqlc.arg(state), sqlc.arg(chunk_ids), sqlc.arg(source_paths), sqlc.arg(approver), sqlc.arg(note), sqlc.arg(opened_at), now())
ON CONFLICT (repo_id, number) DO UPDATE SET url = EXCLUDED.url, branch = EXCLUDED.branch, base = EXCLUDED.base,
    job_id = EXCLUDED.job_id, mode = EXCLUDED.mode, state = EXCLUDED.state, chunk_ids = EXCLUDED.chunk_ids,
    source_paths = EXCLUDED.source_paths, approver = EXCLUDED.approver, note = EXCLUDED.note, updated_at = now();

-- name: GetPRRecord :one
SELECT * FROM prs WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

-- name: ActivePRs :many
SELECT * FROM prs WHERE repo_id = sqlc.arg(repo_id) AND state IN ('open', 'awaiting_review') ORDER BY number;

-- name: AllActivePRs :many
SELECT * FROM prs WHERE state IN ('open', 'awaiting_review') ORDER BY repo_id, number;

-- name: SetPRState :execrows
UPDATE prs SET state = sqlc.arg(state), note = sqlc.arg(note), updated_at = now()
WHERE repo_id = sqlc.arg(repo_id) AND number = sqlc.arg(number);

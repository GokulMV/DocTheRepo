-- name: ChunksForPaths :many
-- Stored chunks (live and soft-deleted) for the paths a push recomputes.
SELECT * FROM chunks WHERE repo_id = sqlc.arg(repo_id) AND source = sqlc.arg(source) AND path = ANY(sqlc.arg(paths)::text[]);

-- name: ChunksByIDs :many
SELECT * FROM chunks WHERE chunk_id = ANY(sqlc.arg(ids)::text[]);

-- name: LiveChunks :many
-- Keyset-paginated walk of live chunks (reindex, exports).
SELECT * FROM chunks WHERE deleted_at IS NULL AND chunk_id > sqlc.arg(after) ORDER BY chunk_id LIMIT sqlc.arg(lim);

-- name: UpsertChunk :exec
INSERT INTO chunks (chunk_id, repo_id, scope, source, path, symbol, language, content, content_hash, signature, commit_sha, url,
                    updated_at, deleted_at)
VALUES (sqlc.arg(chunk_id), sqlc.narg(repo_id), sqlc.arg(scope), sqlc.arg(source), sqlc.arg(path), sqlc.arg(symbol),
        sqlc.arg(language), sqlc.arg(content), sqlc.arg(content_hash), sqlc.arg(signature), sqlc.arg(commit_sha), sqlc.arg(url),
        now(), NULL)
ON CONFLICT (chunk_id) DO UPDATE SET repo_id = EXCLUDED.repo_id, scope = EXCLUDED.scope, source = EXCLUDED.source,
    path = EXCLUDED.path, symbol = EXCLUDED.symbol, language = EXCLUDED.language, content = EXCLUDED.content,
    content_hash = EXCLUDED.content_hash, signature = EXCLUDED.signature, commit_sha = EXCLUDED.commit_sha,
    url = EXCLUDED.url, updated_at = now(), deleted_at = NULL;

-- name: SoftDeleteChunks :execrows
UPDATE chunks SET deleted_at = now(), updated_at = now() WHERE chunk_id = ANY(sqlc.arg(ids)::text[]) AND deleted_at IS NULL;

-- name: ReviveChunks :execrows
UPDATE chunks SET deleted_at = NULL, updated_at = now() WHERE chunk_id = ANY(sqlc.arg(ids)::text[]);

-- name: DeleteChunkIDs :exec
DELETE FROM chunks WHERE chunk_id = ANY(sqlc.arg(ids)::text[]);

-- name: GCChunks :many
DELETE FROM chunks WHERE deleted_at IS NOT NULL AND deleted_at < sqlc.arg(cutoff) RETURNING chunk_id;

-- name: BumpIndexVersion :one
UPDATE index_version SET version = version + 1 RETURNING version;

-- name: GetIndexVersion :one
SELECT version FROM index_version;

-- name: SharedChunksForPath :many
-- Stored chunks (live and soft-deleted) of one repo-less document (a Confluence page or Jira issue).
SELECT * FROM chunks WHERE repo_id IS NULL AND source = sqlc.arg(source) AND path = sqlc.arg(path);

-- name: SoftDeleteSharedPath :execrows
UPDATE chunks SET deleted_at = now(), updated_at = now()
WHERE repo_id IS NULL AND source = sqlc.arg(source) AND path = sqlc.arg(path) AND deleted_at IS NULL;

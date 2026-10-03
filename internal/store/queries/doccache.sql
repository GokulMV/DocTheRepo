-- name: GetDocCache :many
SELECT content_hash, symbol, body FROM doc_cache
WHERE (content_hash, symbol) IN (SELECT unnest(sqlc.arg(hashes)::text[]), unnest(sqlc.arg(symbols)::text[]));

-- name: TouchDocCache :exec
UPDATE doc_cache SET used_at = now()
WHERE (content_hash, symbol) IN (SELECT unnest(sqlc.arg(hashes)::text[]), unnest(sqlc.arg(symbols)::text[]));

-- name: PutDocCache :exec
INSERT INTO doc_cache (content_hash, symbol, body, model) VALUES (sqlc.arg(content_hash), sqlc.arg(symbol), sqlc.arg(body), sqlc.arg(model))
ON CONFLICT (content_hash, symbol) DO UPDATE SET body = EXCLUDED.body, model = EXCLUDED.model, used_at = now();

-- name: GCDocCache :execrows
DELETE FROM doc_cache WHERE used_at < sqlc.arg(cutoff);

-- name: DocumentedChunkIDs :many
-- Chunks that have a generated doc section in the Tree.
SELECT DISTINCT chunk_id::text AS chunk_id FROM doc_nodes WHERE repo_id = sqlc.arg(repo_id) AND kind = 'section' AND chunk_id IS NOT NULL;

-- name: GetAppSetting :one
SELECT value FROM app_settings WHERE key = sqlc.arg(key);

-- name: SetAppSetting :exec
INSERT INTO app_settings (key, value) VALUES (sqlc.arg(key), sqlc.arg(value))
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

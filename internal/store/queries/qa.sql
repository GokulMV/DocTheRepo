-- name: SearchChunksFTS :many
-- Keyword candidates for hybrid retrieval. The English config stems prose; the simple config keeps exact
-- identifiers (function names, error codes). ACL and source filters run in SQL, before ranking.
SELECT c.chunk_id, ts_rank_cd(c.tsv, q.query)::float8 AS rank
FROM chunks c,
     (SELECT websearch_to_tsquery('english', sqlc.arg(question)::text) || websearch_to_tsquery('simple', sqlc.arg(question)::text) AS query) q
WHERE c.deleted_at IS NULL AND c.tsv @@ q.query
  AND (sqlc.arg(all_repos)::boolean OR c.repo_id = ANY(sqlc.arg(repo_ids)::uuid[])
       OR (c.repo_id IS NULL AND c.source IN ('confluence', 'jira')))
  AND (cardinality(sqlc.arg(sources)::text[]) = 0 OR c.source::text = ANY(sqlc.arg(sources)::text[]))
ORDER BY rank DESC, c.chunk_id
LIMIT sqlc.arg(lim);

-- name: ChunksByIDsScoped :many
SELECT * FROM chunks
WHERE chunk_id = ANY(sqlc.arg(ids)::text[]) AND deleted_at IS NULL
  AND (sqlc.arg(all_repos)::boolean OR repo_id = ANY(sqlc.arg(repo_ids)::uuid[])
       OR (repo_id IS NULL AND source IN ('confluence', 'jira')));

-- name: SymbolNeighbors :many
-- One-hop symbol neighbours (callers and callees) of the given symbol entity keys.
SELECT DISTINCT n.key
FROM entities s
JOIN edges x ON (x.src_id = s.id OR x.dst_id = s.id) AND x.deleted_at IS NULL
JOIN entities n ON n.id = CASE WHEN x.src_id = s.id THEN x.dst_id ELSE x.src_id END
WHERE s.kind = 'symbol' AND s.key = ANY(sqlc.arg(keys)::text[]) AND n.kind = 'symbol' AND n.deleted_at IS NULL
  AND NOT (n.key = ANY(sqlc.arg(keys)::text[]))
LIMIT sqlc.arg(lim);

-- name: GetCachedAnswer :one
UPDATE answer_cache SET hits = hits + 1 WHERE key = sqlc.arg(key) AND created_at > now() - interval '24 hours'
RETURNING answer, citations;

-- name: PutCachedAnswer :exec
INSERT INTO answer_cache (key, answer, citations) VALUES (sqlc.arg(key), sqlc.arg(answer), sqlc.arg(citations))
ON CONFLICT (key) DO UPDATE SET answer = EXCLUDED.answer, citations = EXCLUDED.citations, created_at = now();

-- name: GCAnswerCache :execrows
DELETE FROM answer_cache WHERE created_at < now() - interval '24 hours';

-- name: CreateThread :exec
INSERT INTO qa_threads (id, user_id, title, scope) VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(title), sqlc.arg(scope));

-- name: GetThread :one
SELECT * FROM qa_threads WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: TouchThread :exec
UPDATE qa_threads SET updated_at = now() WHERE id = sqlc.arg(id);

-- name: ListThreads :many
SELECT * FROM qa_threads WHERE user_id = sqlc.arg(user_id)
  AND (sqlc.narg(before)::timestamptz IS NULL OR updated_at < sqlc.narg(before))
ORDER BY updated_at DESC LIMIT sqlc.arg(lim);

-- name: DeleteThread :execrows
DELETE FROM qa_threads WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: InsertMessage :exec
INSERT INTO qa_messages (id, thread_id, role, content, citations, provider, model, input_tokens, output_tokens, cost_usd, cached)
VALUES (sqlc.arg(id), sqlc.arg(thread_id), sqlc.arg(role), sqlc.arg(content), sqlc.arg(citations), sqlc.arg(provider), sqlc.arg(model),
        sqlc.arg(input_tokens), sqlc.arg(output_tokens), sqlc.arg(cost_usd), sqlc.arg(cached));

-- name: ThreadMessages :many
-- A question and its answer are saved in one transaction (same created_at): the question comes first.
SELECT * FROM qa_messages WHERE thread_id = sqlc.arg(thread_id) ORDER BY created_at, (role <> 'user'), id;

-- name: SetFeedback :execrows
UPDATE qa_messages m SET feedback = sqlc.arg(feedback), feedback_comment = sqlc.arg(comment)
FROM qa_threads t
WHERE m.id = sqlc.arg(id) AND m.thread_id = t.id AND t.user_id = sqlc.arg(user_id) AND m.role = 'assistant';

-- name: OverviewChunks :many
-- Material that describes a repository as a whole, for broad questions ("how does this repo work?"):
-- READMEs and architecture/overview docs first, then generated docs of entry points.
SELECT c.chunk_id,
       (CASE WHEN lower(c.path) ~ '(^|/)readme' THEN 3
             WHEN lower(c.path) ~ '(architecture|overview|design|getting[-_]started|introduction)' THEN 2
             ELSE 1 END)::float8 AS rank
FROM chunks c
WHERE c.deleted_at IS NULL
  AND (sqlc.arg(all_repos)::boolean OR c.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
  AND (cardinality(sqlc.arg(sources)::text[]) = 0 OR c.source::text = ANY(sqlc.arg(sources)::text[]))
  AND (lower(c.path) ~ '(^|/)readme'
       OR lower(c.path) ~ '(architecture|overview|design|getting[-_]started|introduction)'
       OR (c.source = 'generated_doc' AND lower(c.path) ~ '(^|/)(main|index|app|server|cli|cmd)[^/]*\.md$'))
ORDER BY rank DESC, length(c.path), c.chunk_id
LIMIT sqlc.arg(lim);

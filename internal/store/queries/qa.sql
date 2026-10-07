-- name: SearchChunksFTS :many
-- Keyword candidates for hybrid retrieval. The English config stems prose; the simple config keeps exact
-- identifiers (function names, error codes). ACL and source filters run in SQL, before ranking.
SELECT c.chunk_id, ts_rank_cd(c.tsv, q.query)::float8 AS rank
FROM chunks c,
     (SELECT websearch_to_tsquery('english', sqlc.arg(question)::text) || websearch_to_tsquery('simple', sqlc.arg(question)::text) AS query) q
WHERE c.deleted_at IS NULL AND c.tsv @@ q.query
  AND (sqlc.arg(all_repos)::boolean OR c.repo_id = ANY(sqlc.arg(repo_ids)::uuid[])
       OR (c.repo_id IS NULL AND c.source IN ('confluence', 'jira', 'notion', 'upload')))
  AND (cardinality(sqlc.arg(sources)::text[]) = 0 OR c.source::text = ANY(sqlc.arg(sources)::text[]))
ORDER BY rank DESC, c.chunk_id
LIMIT sqlc.arg(lim);

-- name: ChunksByIDsScoped :many
SELECT * FROM chunks
WHERE chunk_id = ANY(sqlc.arg(ids)::text[]) AND deleted_at IS NULL
  AND (sqlc.arg(all_repos)::boolean OR repo_id = ANY(sqlc.arg(repo_ids)::uuid[])
       OR (repo_id IS NULL AND source IN ('confluence', 'jira', 'notion', 'upload')));

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
-- A cached answer is served while every chunk it cites still exists and nothing in the repositories or
-- spaces it cites changed since it was cached.
UPDATE answer_cache a SET hits = hits + 1
WHERE a.key = sqlc.arg(key) AND a.created_at > now() - interval '7 days'
  AND (SELECT count(*) FROM chunks c WHERE c.chunk_id = ANY(a.chunk_ids) AND c.deleted_at IS NULL) = cardinality(a.chunk_ids)
  AND NOT EXISTS (SELECT 1 FROM chunks c WHERE c.scope = ANY(a.scopes) AND c.updated_at > a.created_at)
RETURNING answer, citations;

-- name: PutCachedAnswer :exec
INSERT INTO answer_cache (key, answer, citations, chunk_ids, scopes)
VALUES (sqlc.arg(key), sqlc.arg(answer), sqlc.arg(citations), sqlc.arg(chunk_ids)::text[],
        (SELECT coalesce(array_agg(DISTINCT c.scope), '{}')::text[] FROM chunks c WHERE c.chunk_id = ANY(sqlc.arg(chunk_ids)::text[])))
ON CONFLICT (key) DO UPDATE SET answer = EXCLUDED.answer, citations = EXCLUDED.citations, chunk_ids = EXCLUDED.chunk_ids,
    scopes = EXCLUDED.scopes, created_at = now(), hits = 0;

-- name: SimilarAnswerCandidates :many
-- Fresh answers asked in the same scope with the same embedding model, newest first (same freshness rules
-- as GetCachedAnswer); the Hub compares the embeddings.
SELECT a.key, a.question, a.embedding, a.answer, a.citations
FROM answer_cache a
WHERE a.scope_key = sqlc.arg(scope_key) AND a.embed_model = sqlc.arg(embed_model) AND a.embedding IS NOT NULL
  AND a.created_at > now() - interval '7 days'
  AND (SELECT count(*) FROM chunks c WHERE c.chunk_id = ANY(a.chunk_ids) AND c.deleted_at IS NULL) = cardinality(a.chunk_ids)
  AND NOT EXISTS (SELECT 1 FROM chunks c WHERE c.scope = ANY(a.scopes) AND c.updated_at > a.created_at)
ORDER BY a.created_at DESC
LIMIT sqlc.arg(lim);

-- name: HitCachedAnswer :exec
UPDATE answer_cache SET hits = hits + 1 WHERE key = sqlc.arg(key);

-- name: SetAnswerMeaning :exec
UPDATE answer_cache SET question = sqlc.arg(question), scope_key = sqlc.arg(scope_key), embed_model = sqlc.arg(embed_model),
    embedding = sqlc.arg(embedding)::real[]
WHERE key = sqlc.arg(key);

-- name: GCAnswerCache :execrows
DELETE FROM answer_cache WHERE created_at < now() - interval '7 days';

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
INSERT INTO qa_messages (id, thread_id, role, content, citations, provider, model, input_tokens, output_tokens, cost_usd, cached, investigated, sift)
VALUES (sqlc.arg(id), sqlc.arg(thread_id), sqlc.arg(role), sqlc.arg(content), sqlc.arg(citations), sqlc.arg(provider), sqlc.arg(model),
        sqlc.arg(input_tokens), sqlc.arg(output_tokens), sqlc.arg(cost_usd), sqlc.arg(cached), sqlc.arg(investigated), sqlc.narg(sift));

-- name: ThreadMessages :many
-- A question and its answer are saved in one transaction (same created_at): the question comes first.
SELECT * FROM qa_messages WHERE thread_id = sqlc.arg(thread_id) ORDER BY created_at, (role <> 'user'), id;

-- name: SetFeedback :execrows
UPDATE qa_messages m SET feedback = sqlc.arg(feedback), feedback_comment = sqlc.arg(comment)
FROM qa_threads t
WHERE m.id = sqlc.arg(id) AND m.thread_id = t.id AND t.user_id = sqlc.arg(user_id) AND m.role = 'assistant';

-- name: OverviewChunks :many
-- Material that describes a repository as a whole, for broad questions ("how does this repo work?"):
-- the Hub's Overview and Architecture documents first, then READMEs and architecture/overview docs, then
-- generated docs of entry points.
SELECT c.chunk_id,
       (CASE WHEN c.path IN ('@docs/overview/overview', '@docs/architecture/architecture') THEN 4
             WHEN lower(c.path) ~ '(^|/)readme' THEN 3
             WHEN lower(c.path) ~ '(architecture|overview|design|getting[-_]started|introduction)' THEN 2
             ELSE 1 END)::float8 AS rank
FROM chunks c
WHERE c.deleted_at IS NULL
  AND (sqlc.arg(all_repos)::boolean OR c.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
  AND (cardinality(sqlc.arg(sources)::text[]) = 0 OR c.source::text = ANY(sqlc.arg(sources)::text[]))
  AND (c.path IN ('@docs/overview/overview', '@docs/architecture/architecture')
       OR lower(c.path) ~ '(^|/)readme'
       OR lower(c.path) ~ '(architecture|overview|design|getting[-_]started|introduction)'
       OR (c.source = 'generated_doc' AND lower(c.path) ~ '(^|/)(main|index|app|server|cli|cmd)[^/]*\.md$'))
ORDER BY rank DESC, length(c.path), c.chunk_id
LIMIT sqlc.arg(lim);

-- name: ChunksForPath :many
-- The agent's "read a file": the chunks of files whose path is, or ends with, the given one.
SELECT * FROM chunks c
WHERE c.deleted_at IS NULL AND (c.path = sqlc.arg(path)::text OR c.path LIKE '%/' || sqlc.arg(path)::text)
  AND (sqlc.arg(all_repos)::boolean OR c.repo_id = ANY(sqlc.arg(repo_ids)::uuid[])
       OR (c.repo_id IS NULL AND c.source IN ('confluence', 'jira', 'notion', 'upload')))
  AND (cardinality(sqlc.arg(sources)::text[]) = 0 OR c.source::text = ANY(sqlc.arg(sources)::text[]))
ORDER BY c.scope, c.path, c.chunk_id
LIMIT sqlc.arg(lim);

-- name: ListChunkPaths :many
-- The agent's "list files": distinct paths containing the given text, with how many chunks each has.
SELECT c.scope, c.path, count(*)::int AS chunks FROM chunks c
WHERE c.deleted_at IS NULL AND c.path ILIKE '%' || sqlc.arg(contains)::text || '%'
  AND (sqlc.arg(all_repos)::boolean OR c.repo_id = ANY(sqlc.arg(repo_ids)::uuid[])
       OR (c.repo_id IS NULL AND c.source IN ('confluence', 'jira', 'notion', 'upload')))
  AND (cardinality(sqlc.arg(sources)::text[]) = 0 OR c.source::text = ANY(sqlc.arg(sources)::text[]))
GROUP BY c.scope, c.path
ORDER BY c.scope, c.path
LIMIT sqlc.arg(lim);

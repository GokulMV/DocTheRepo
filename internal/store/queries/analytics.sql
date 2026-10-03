-- name: UsageSeries :many
-- Usage bucketed by hour or day and grouped by one dimension (plan § 7.7).
SELECT date_trunc(sqlc.arg(granularity)::text, at)::timestamptz AS t,
       (CASE sqlc.arg(group_by)::text
            WHEN 'feature' THEN feature::text
            WHEN 'provider' THEN provider_kind::text
            WHEN 'model' THEN model
            WHEN 'repo' THEN coalesce(repo_id::text, '')
            WHEN 'user' THEN coalesce(user_id::text, '')
            ELSE 'all' END)::text AS key,
       count(*)::bigint AS calls,
       coalesce(sum(input_tokens + output_tokens), 0)::bigint AS tokens,
       coalesce(sum(cost_usd), 0)::float8 AS cost_usd,
       coalesce(sum(cache_read_tokens), 0)::bigint AS cache_read_tokens,
       count(*) FILTER (WHERE cached)::bigint AS cached_calls,
       count(*) FILTER (WHERE outcome = 'blocked')::bigint AS blocked
FROM usage_events
WHERE at >= sqlc.arg(from_t) AND at < sqlc.arg(to_t)
  AND (sqlc.narg(user_id)::uuid IS NULL OR user_id = sqlc.narg(user_id))
GROUP BY 1, 2 ORDER BY 1, 2;

-- name: SavingsByKind :many
SELECT kind::text AS kind, count(*)::bigint AS events, coalesce(sum(est_tokens_avoided), 0)::bigint AS tokens_avoided,
       coalesce(sum(est_cost_avoided_usd), 0)::float8 AS cost_avoided_usd
FROM savings_events WHERE at >= sqlc.arg(from_t) AND at < sqlc.arg(to_t) GROUP BY kind ORDER BY kind;

-- name: PipelineStats :many
SELECT type, status::text AS status, count(*)::bigint AS jobs,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM updated_at - created_at)), 0)::float8 AS p50_seconds,
       coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY extract(epoch FROM updated_at - created_at)), 0)::float8 AS p95_seconds
FROM jobs WHERE created_at >= sqlc.arg(from_t) AND created_at < sqlc.arg(to_t)
GROUP BY type, status ORDER BY type, status;

-- name: RepoFreshness :many
SELECT r.id, r.full_name, r.last_processed_sha, r.updated_at,
       (SELECT max(j.updated_at) FROM jobs j WHERE j.repo_id = r.id AND j.type = 'code_push' AND j.status IN ('done', 'aborted', 'pending_approval')) AS last_success
FROM repos r WHERE r.enabled ORDER BY r.full_name;

-- name: ConnectorStats :many
SELECT c.id, c.type::text AS type, c.name, c.health::text AS health, c.last_sync_at, c.last_error,
       (SELECT count(*) FROM jobs j JOIN repos r ON r.id = j.repo_id WHERE r.connector_id = c.id AND j.created_at > now() - interval '1 day')::bigint AS jobs_day
FROM connectors c ORDER BY c.name;

-- name: ActivityJobs :many
SELECT j.id, j.type, j.status::text AS status, j.repo_id, j.error, j.updated_at
FROM jobs j
WHERE j.status NOT IN ('queued', 'processing') AND j.updated_at >= sqlc.arg(since) AND j.updated_at < sqlc.arg(before)
  AND (j.repo_id IS NULL OR sqlc.arg(all_repos)::boolean OR j.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
ORDER BY j.updated_at DESC LIMIT sqlc.arg(lim);

-- name: ActivityPRs :many
SELECT p.repo_id, p.number, p.state, p.url, p.updated_at
FROM prs p
WHERE p.updated_at >= sqlc.arg(since) AND p.updated_at < sqlc.arg(before)
  AND (sqlc.arg(all_repos)::boolean OR p.repo_id = ANY(sqlc.arg(repo_ids)::uuid[]))
ORDER BY p.updated_at DESC LIMIT sqlc.arg(lim);

-- name: ActivityAudit :many
SELECT a.id, a.action, a.target_type, a.target_id, a.at FROM audit_log a
WHERE a.at >= sqlc.arg(since) AND a.at < sqlc.arg(before) ORDER BY a.at DESC LIMIT sqlc.arg(lim);

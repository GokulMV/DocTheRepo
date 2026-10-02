-- name: EnqueueJob :one
INSERT INTO jobs (id, type, repo_id, serial_key, dedupe_key, payload, priority, max_attempts, run_after,
                  correlation_id, replayed_from)
VALUES (sqlc.arg(id), sqlc.arg(type), sqlc.narg(repo_id), sqlc.narg(serial_key), sqlc.narg(dedupe_key),
        sqlc.arg(payload), sqlc.arg(priority), sqlc.arg(max_attempts), sqlc.arg(run_after),
        sqlc.arg(correlation_id), sqlc.narg(replayed_from))
ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('queued', 'processing') DO NOTHING
RETURNING *;

-- name: GetLiveJobByDedupeKey :one
SELECT * FROM jobs
WHERE dedupe_key = sqlc.arg(dedupe_key) AND status IN ('queued', 'processing');

-- name: ClaimJob :one
-- Claims the next runnable job of a type. A job with a serial key is runnable only when no earlier job
-- with the same key is still queued or processing, which serializes e.g. pushes to one repo while jobs
-- for different keys run in parallel. SKIP LOCKED makes concurrent claims exactly-once.
WITH cand AS (
    SELECT j.id
    FROM jobs j
    WHERE j.status = 'queued'
      AND j.type = sqlc.arg(job_type)
      AND j.run_after <= now()
      AND (j.serial_key IS NULL OR NOT EXISTS (
            SELECT 1 FROM jobs e
            WHERE e.serial_key = j.serial_key
              AND e.seq < j.seq
              AND e.status IN ('queued', 'processing')))
    ORDER BY j.priority DESC, j.seq
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE jobs
SET status = 'processing',
    locked_by = sqlc.arg(worker),
    locked_until = now() + make_interval(secs => sqlc.arg(ttl_seconds)::float8),
    attempts = jobs.attempts + 1,
    updated_at = now()
FROM cand
WHERE jobs.id = cand.id
RETURNING jobs.*;

-- name: HeartbeatJob :execrows
UPDATE jobs
SET locked_until = now() + make_interval(secs => sqlc.arg(ttl_seconds)::float8), updated_at = now()
WHERE id = sqlc.arg(id) AND locked_by = sqlc.arg(worker) AND status = 'processing';

-- name: FinishJob :execrows
-- Fenced by locked_by so a worker whose lease was reclaimed cannot overwrite the new owner's state.
UPDATE jobs
SET status = sqlc.arg(status), result = sqlc.narg(result), error = sqlc.arg(error),
    locked_by = NULL, locked_until = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND locked_by = sqlc.arg(worker) AND status = 'processing';

-- name: RescheduleJob :execrows
UPDATE jobs
SET status = 'queued', run_after = sqlc.arg(run_after), error = sqlc.arg(error),
    attempts = jobs.attempts - sqlc.arg(refund_attempts)::int,
    locked_by = NULL, locked_until = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND locked_by = sqlc.arg(worker) AND status = 'processing';

-- name: ReclaimExpiredLeases :many
UPDATE jobs
SET status = CASE WHEN attempts >= max_attempts THEN 'dead'::job_status ELSE 'queued'::job_status END,
    error = 'lease expired: worker stopped heartbeating',
    locked_by = NULL, locked_until = NULL, run_after = now(), updated_at = now()
WHERE status = 'processing' AND locked_until < now()
RETURNING id, type, status;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = sqlc.arg(id);

-- name: ListJobs :many
SELECT * FROM jobs
WHERE (sqlc.narg(status)::job_status IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(type)::text IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(repo_id)::uuid IS NULL OR repo_id = sqlc.narg(repo_id))
  AND (sqlc.narg(before_seq)::bigint IS NULL OR seq < sqlc.narg(before_seq))
ORDER BY seq DESC
LIMIT sqlc.arg(max_rows);

-- name: QueueDepth :many
SELECT type, count(*)::bigint AS depth FROM jobs WHERE status = 'queued' GROUP BY type;

-- name: NotifyJobs :exec
SELECT pg_notify('dth_jobs', sqlc.arg(job_type)::text);

-- name: SetJobProgress :execrows
UPDATE jobs SET progress = sqlc.arg(progress), updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'processing';

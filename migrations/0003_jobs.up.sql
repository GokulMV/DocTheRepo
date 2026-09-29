-- Postgres-backed job queue (plan § 6.5, § 8.15).
CREATE TYPE job_status AS ENUM ('queued', 'processing', 'done', 'failed', 'aborted', 'spend_blocked',
                                'pending_approval', 'needs_human', 'dead');

CREATE TABLE jobs (
    id              uuid PRIMARY KEY,
    seq             bigserial NOT NULL UNIQUE,          -- enqueue order; defines per-serial-key ordering
    type            text NOT NULL,
    repo_id         uuid REFERENCES repos(id) ON DELETE SET NULL,
    serial_key      text,                               -- jobs sharing a key run one at a time, in seq order
    dedupe_key      text,
    payload         jsonb NOT NULL DEFAULT '{}',
    status          job_status NOT NULL DEFAULT 'queued',
    priority        smallint NOT NULL DEFAULT 0,
    attempts        integer NOT NULL DEFAULT 0,
    max_attempts    integer NOT NULL DEFAULT 5,
    run_after       timestamptz NOT NULL DEFAULT now(),
    locked_by       text,
    locked_until    timestamptz,
    correlation_id  text NOT NULL,
    error           text NOT NULL DEFAULT '',
    result          jsonb,
    replayed_from   uuid REFERENCES jobs(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Claim path: queued jobs of a type, highest priority then oldest first.
CREATE INDEX jobs_claim_idx ON jobs (type, priority DESC, seq) WHERE status = 'queued';
-- Ordering predicate: "is there an earlier unfinished job with my serial key?"
CREATE INDEX jobs_serial_idx ON jobs (serial_key, seq) WHERE serial_key IS NOT NULL AND status IN ('queued', 'processing');
-- Lease reclaim sweep.
CREATE INDEX jobs_lease_idx ON jobs (locked_until) WHERE status = 'processing';
-- At most one live job per dedupe key.
CREATE UNIQUE INDEX jobs_dedupe_idx ON jobs (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('queued', 'processing');
-- Listing/filtering for the API and activity feed.
CREATE INDEX jobs_status_idx ON jobs (status, created_at DESC);
CREATE INDEX jobs_repo_idx ON jobs (repo_id, created_at DESC);

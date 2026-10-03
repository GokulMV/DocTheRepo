-- Security scans powered by kryptonite's attack modules (internal/core/security). A scan reads a repository's
-- code statically, asks the security route to attack it module by module, verifies every candidate in a
-- second pass, and stores the findings. Nothing changes in the repository until someone selects findings
-- and clicks Fix, which opens a pull request.
ALTER TYPE llm_feature ADD VALUE IF NOT EXISTS 'security';

CREATE TABLE security_scans (
    id            uuid PRIMARY KEY,
    repo_id       uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    job_id        uuid,
    status        text NOT NULL DEFAULT 'queued',      -- queued | running | done | failed
    commit_sha    text NOT NULL DEFAULT '',
    modules       text[] NOT NULL,
    verdict       text NOT NULL DEFAULT '',            -- GO | NO-GO (kryptonite's readiness call)
    summary       jsonb NOT NULL DEFAULT '{}',         -- counts by priority/status, files read, tokens, notes
    error         text NOT NULL DEFAULT '',
    started_by    uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz
);
CREATE INDEX security_scans_repo_idx ON security_scans (repo_id, created_at DESC);

CREATE TABLE security_findings (
    id              uuid PRIMARY KEY,
    scan_id         uuid NOT NULL REFERENCES security_scans(id) ON DELETE CASCADE,
    repo_id         uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    dimension       text NOT NULL,                     -- the kryptonite module
    title           text NOT NULL,
    surface         text NOT NULL DEFAULT '',
    file            text NOT NULL DEFAULT '',
    line_start      integer NOT NULL DEFAULT 0,
    line_end        integer NOT NULL DEFAULT 0,
    repro           jsonb NOT NULL DEFAULT '[]',
    evidence        text NOT NULL DEFAULT '',
    severity        text NOT NULL,                     -- critical | high | medium | low | info
    exploitability  text NOT NULL,                     -- trivial | easy | moderate | hard
    priority        text NOT NULL,                     -- P0..P3 (kryptonite's matrix)
    status          text NOT NULL,                     -- confirmed | plausible | rejected
    fix_status      text NOT NULL DEFAULT '',          -- '' | queued | proposed | pr_opened | failed
    fix             jsonb NOT NULL DEFAULT '{}',       -- the proposal: files, tests, risk
    fix_pr_url      text NOT NULL DEFAULT '',
    fix_error       text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX security_findings_scan_idx ON security_findings (scan_id);

-- A module's verified findings for an exact set of inputs: a re-scan of unchanged code costs nothing.
CREATE TABLE security_module_cache (
    repo_id      uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    module       text NOT NULL,
    inputs_hash  text NOT NULL,
    findings     jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, module, inputs_hash)
);

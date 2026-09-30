-- Signals, issues, decodes, known issues (plan § 6.4).

CREATE TYPE signal_severity AS ENUM ('info', 'warning', 'error', 'critical'); -- declaration order = rank
CREATE TYPE signal_kind AS ENUM ('error', 'alert', 'security_finding', 'log_match', 'event_bus');
CREATE TYPE issue_status AS ENUM ('new', 'decoded', 'suppressed', 'acknowledged', 'resolved', 'regressed');
CREATE TYPE known_issue_reason AS ENUM ('known_bug', 'wont_fix', 'third_party', 'expected_noise', 'cannot_action', 'in_progress');
CREATE TYPE known_issue_action AS ENUM ('suppress', 'label_only');
CREATE TYPE known_issue_source AS ENUM ('manual', 'suggested', 'confluence', 'jira', 'pasted');
CREATE TYPE decode_confidence AS ENUM ('high', 'medium', 'low');
CREATE TYPE suggestion_status AS ENUM ('pending', 'accepted', 'rejected');

CREATE TABLE known_issues (
    id                  uuid PRIMARY KEY,
    title               text NOT NULL,
    description         text NOT NULL DEFAULT '',
    explanation         text NOT NULL DEFAULT '',   -- Hub-generated plain-English explanation
    source_text         text NOT NULL DEFAULT '',   -- pasted text or fetched Jira/Confluence body (scrubbed)
    jira_key            text,
    reason              known_issue_reason NOT NULL,
    match               jsonb NOT NULL,             -- knownissues.Match (scope lives here: services/environments/sources)
    action              known_issue_action NOT NULL DEFAULT 'suppress',
    enabled             boolean NOT NULL DEFAULT true,  -- suggested rules start disabled until a human approves
    expires_at          timestamptz,
    source              known_issue_source NOT NULL DEFAULT 'manual',
    confluence_page_id  text,
    owner_user_id       uuid REFERENCES users(id) ON DELETE SET NULL,
    ticket_url          text NOT NULL DEFAULT '',
    hits                bigint NOT NULL DEFAULT 0,
    last_hit_at         timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX known_issues_enabled_idx ON known_issues (enabled) WHERE enabled;

CREATE TABLE issues (
    id                uuid PRIMARY KEY,
    fingerprint       text NOT NULL UNIQUE,
    alt_fingerprint   text NOT NULL DEFAULT '',      -- content fingerprint when the source grouped for us
    kind              signal_kind NOT NULL,
    title             text NOT NULL,
    service           text NOT NULL,
    environment       text NOT NULL DEFAULT '',
    repo_id           uuid REFERENCES repos(id) ON DELETE SET NULL,  -- via service_map; issues inherit its ACL
    first_seen        timestamptz NOT NULL,
    last_seen         timestamptz NOT NULL,
    occurrences       bigint NOT NULL DEFAULT 0,     -- not suppressed
    suppressed_count  bigint NOT NULL DEFAULT 0,
    sources           text[] NOT NULL DEFAULT '{}',
    status            issue_status NOT NULL DEFAULT 'new',
    severity_max      signal_severity NOT NULL,
    known_issue_id    uuid REFERENCES known_issues(id) ON DELETE SET NULL,
    decode_id         uuid,
    assignee_user_id  uuid REFERENCES users(id) ON DELETE SET NULL,
    resolved_at       timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX issues_status_idx ON issues (status, last_seen DESC);
CREATE INDEX issues_last_seen_idx ON issues (last_seen DESC);
CREATE INDEX issues_service_idx ON issues (service);
CREATE INDEX issues_alt_fp_idx ON issues (alt_fingerprint) WHERE alt_fingerprint <> '';
CREATE INDEX issues_known_idx ON issues (known_issue_id) WHERE known_issue_id IS NOT NULL;

CREATE TABLE decodes (
    id                    uuid PRIMARY KEY,
    issue_id              uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    summary               text NOT NULL,
    probable_cause        text NOT NULL DEFAULT '',
    impact                text NOT NULL DEFAULT '',
    affected_code         jsonb NOT NULL DEFAULT '[]',  -- [{repo, path, symbol, lines, chunk_id}]
    related_commits       jsonb NOT NULL DEFAULT '[]',
    related_docs          jsonb NOT NULL DEFAULT '[]',
    similar_issue_ids     uuid[] NOT NULL DEFAULT '{}',
    next_steps            text[] NOT NULL DEFAULT '{}',
    confidence            decode_confidence NOT NULL,
    is_actionable         boolean NOT NULL DEFAULT true,
    suggest_known_issue   boolean NOT NULL DEFAULT false,
    provider              text NOT NULL DEFAULT '',
    model                 text NOT NULL DEFAULT '',
    tokens                bigint NOT NULL DEFAULT 0,
    cost_usd              numeric(14, 6) NOT NULL DEFAULT 0,
    fingerprint_version   integer NOT NULL,
    created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX decodes_issue_idx ON decodes (issue_id, created_at DESC);
CREATE INDEX decodes_created_idx ON decodes (created_at DESC);
ALTER TABLE issues ADD CONSTRAINT issues_decode_fk FOREIGN KEY (decode_id) REFERENCES decodes(id) ON DELETE SET NULL;

-- Samples only (first 5 per fingerprint + a reservoir of 20 per fingerprint per hour), never every event.
-- Reservoir samples overwrite their (hour, slot); first samples use slots -1..-5. Partitioned by day and
-- retained 30 days (partition drop).
CREATE TABLE event_samples (
    issue_id          uuid NOT NULL,
    sample_day        date NOT NULL,
    sample_hour       timestamptz NOT NULL,
    sample_slot       smallint NOT NULL,
    sample_kind       text NOT NULL,        -- first | reservoir
    connector_id      uuid,
    source            text NOT NULL,
    external_id       text NOT NULL,
    occurred_at       timestamptz NOT NULL,
    received_at       timestamptz NOT NULL,
    severity          signal_severity NOT NULL,
    kind              signal_kind NOT NULL,
    service           text NOT NULL,
    environment       text NOT NULL DEFAULT '',
    title             text NOT NULL,
    message_scrubbed  text NOT NULL DEFAULT '',
    exception_type    text NOT NULL DEFAULT '',
    stack             jsonb NOT NULL DEFAULT '[]',
    attrs             jsonb NOT NULL DEFAULT '{}',
    fingerprint       text NOT NULL,
    PRIMARY KEY (issue_id, sample_day, sample_hour, sample_slot)
) PARTITION BY RANGE (sample_day);
CREATE INDEX event_samples_issue_time_idx ON event_samples (issue_id, occurred_at DESC);

-- Per-minute counts power sparklines and "spiking" (30 days); hourly counts are kept for a year.
CREATE TABLE issue_counts_minutely (
    issue_id          uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    minute            timestamptz NOT NULL,
    count             bigint NOT NULL DEFAULT 0,
    suppressed_count  bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (issue_id, minute)
);
CREATE INDEX issue_counts_minutely_minute_idx ON issue_counts_minutely (minute);
CREATE TABLE issue_counts_hourly (
    issue_id          uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    hour              timestamptz NOT NULL,
    count             bigint NOT NULL DEFAULT 0,
    suppressed_count  bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (issue_id, hour)
);
CREATE INDEX issue_counts_hourly_hour_idx ON issue_counts_hourly (hour);

CREATE TABLE known_issue_suggestions (
    id              uuid PRIMARY KEY,
    issue_ids       uuid[] NOT NULL,
    proposed_match  jsonb NOT NULL,
    rationale       text NOT NULL DEFAULT '',
    status          suggestion_status NOT NULL DEFAULT 'pending',
    decided_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    decided_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX known_issue_suggestions_status_idx ON known_issue_suggestions (status, created_at DESC);

-- ensure_event_sample_partitions creates the daily partitions from `from_day` for `days` days (idempotent).
CREATE FUNCTION ensure_event_sample_partitions(from_day date, days integer) RETURNS void LANGUAGE plpgsql AS $$
DECLARE d date;
BEGIN
    FOR i IN 0..days - 1 LOOP
        d := from_day + i;
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF event_samples FOR VALUES FROM (%L) TO (%L)',
                       'event_samples_' || to_char(d, 'YYYYMMDD'), d, d + 1);
    END LOOP;
END $$;

SELECT ensure_event_sample_partitions((now() AT TIME ZONE 'utc')::date - 1, 9);

-- Usage accounting, savings, spend limits (plan § 6.5).
CREATE TYPE usage_outcome AS ENUM ('ok', 'error', 'blocked');

CREATE TABLE usage_events (
    id             uuid NOT NULL,
    at             timestamptz NOT NULL DEFAULT now(),
    feature        llm_feature NOT NULL,
    provider_kind  llm_provider_kind NOT NULL,
    model          text NOT NULL,
    input_tokens   bigint NOT NULL DEFAULT 0,
    output_tokens  bigint NOT NULL DEFAULT 0,
    cost_usd       numeric(14, 6) NOT NULL DEFAULT 0,
    latency_ms     integer NOT NULL DEFAULT 0,
    cached         boolean NOT NULL DEFAULT false,
    estimated      boolean NOT NULL DEFAULT false,  -- true when the provider reported no usage
    repo_id        uuid,
    user_id        uuid,
    job_id         uuid,
    issue_id       uuid,
    outcome        usage_outcome NOT NULL DEFAULT 'ok',
    PRIMARY KEY (id, at)
) PARTITION BY RANGE (at);

-- Creates monthly partitions of a range-partitioned table from the current month through months_ahead.
CREATE FUNCTION ensure_month_partitions(tbl text, months_ahead integer) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    m     date := date_trunc('month', now())::date;
    i     integer;
    lo    date;
    hi    date;
    pname text;
BEGIN
    FOR i IN 0..months_ahead LOOP
        lo := (m + make_interval(months => i))::date;
        hi := (m + make_interval(months => i + 1))::date;
        pname := format('%s_%s', tbl, to_char(lo, 'YYYYMM'));
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       pname, tbl, lo, hi);
    END LOOP;
END $$;

SELECT ensure_month_partitions('usage_events', 3);
CREATE INDEX usage_events_at_idx ON usage_events (at DESC);
CREATE INDEX usage_events_feature_idx ON usage_events (feature, at DESC);

CREATE TABLE usage_rollups_hourly (
    hour           timestamptz NOT NULL,
    feature        llm_feature NOT NULL,
    provider_kind  llm_provider_kind NOT NULL,
    model          text NOT NULL,
    repo_id        uuid,
    repo_key       uuid GENERATED ALWAYS AS (coalesce(repo_id, '00000000-0000-0000-0000-000000000000')) STORED,
    calls          bigint NOT NULL DEFAULT 0,
    input_tokens   bigint NOT NULL DEFAULT 0,
    output_tokens  bigint NOT NULL DEFAULT 0,
    cost_usd       numeric(14, 6) NOT NULL DEFAULT 0,
    cached_calls   bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (hour, feature, provider_kind, model, repo_key)
);

CREATE TYPE savings_kind AS ENUM ('triage_abort', 'known_issue_suppressed', 'decode_reused', 'answer_cache_hit', 'rename_rekey');

CREATE TABLE savings_events (
    id                    uuid PRIMARY KEY,
    at                    timestamptz NOT NULL DEFAULT now(),
    kind                  savings_kind NOT NULL,
    est_tokens_avoided    bigint NOT NULL DEFAULT 0,
    est_cost_avoided_usd  numeric(14, 6) NOT NULL DEFAULT 0,
    ref_id                text NOT NULL DEFAULT ''
);
CREATE INDEX savings_events_at_idx ON savings_events (at DESC, kind);

CREATE TYPE spend_scope AS ENUM ('global', 'feature', 'provider', 'repo');
CREATE TYPE spend_window AS ENUM ('day', 'month');
CREATE TYPE breach_action AS ENUM ('block', 'block_and_alert');

CREATE TABLE spend_limits (
    id            uuid PRIMARY KEY,
    scope         spend_scope NOT NULL,
    scope_key     text NOT NULL DEFAULT '',     -- feature name, provider id, or repo id; '' for global
    time_window   spend_window NOT NULL DEFAULT 'day',
    max_tokens    bigint,                       -- NULL/0 = unlimited (requires spend.allow_unlimited)
    max_cost_usd  numeric(14, 6),
    on_breach     breach_action NOT NULL DEFAULT 'block',
    alert_url     text NOT NULL DEFAULT '',
    UNIQUE (scope, scope_key, time_window)
);

-- Conservative default carried from the original plan: 2M tokens per day across all features.
INSERT INTO spend_limits (id, scope, scope_key, time_window, max_tokens)
VALUES ('00000000-0000-7000-8000-000000000001', 'global', '', 'day', 2000000);

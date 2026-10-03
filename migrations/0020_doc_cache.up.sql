-- Generated doc sections by the content they describe, so the same code is never documented twice: a
-- retried job, a reverted change, a moved function or a second repository with the same code reuses it.
CREATE TABLE doc_cache (
    content_hash  text NOT NULL,
    symbol        text NOT NULL,
    body          text NOT NULL,
    model         text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    used_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (content_hash, symbol)
);
CREATE INDEX doc_cache_used_idx ON doc_cache (used_at);

-- Docs written without a model call: reused from the cache or an unchanged section, or taken from the
-- code's own comment.
ALTER TYPE savings_kind ADD VALUE IF NOT EXISTS 'doc_reused';
-- A cheaper model for short code (optional route; docgen serves it when unset).
ALTER TYPE llm_feature ADD VALUE IF NOT EXISTS 'docgen_fast';
ALTER TYPE savings_kind ADD VALUE IF NOT EXISTS 'doc_no_call';

-- Hub-wide settings changed in the UI (key → value). Empty: the deployment's config applies.
CREATE TABLE app_settings (
    key         text PRIMARY KEY,
    value       text NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

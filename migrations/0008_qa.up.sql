-- Q&A threads, messages, and the answer cache (plan § 6.5, § 8.12).
CREATE TABLE qa_threads (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       text NOT NULL,
    scope       jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX qa_threads_user_idx ON qa_threads (user_id, updated_at DESC);

CREATE TYPE qa_role AS ENUM ('user', 'assistant');
CREATE TYPE qa_feedback AS ENUM ('up', 'down');

CREATE TABLE qa_messages (
    id                uuid PRIMARY KEY,
    thread_id         uuid NOT NULL REFERENCES qa_threads(id) ON DELETE CASCADE,
    role              qa_role NOT NULL,
    content           text NOT NULL,
    citations         jsonb NOT NULL DEFAULT '[]',
    provider          text NOT NULL DEFAULT '',
    model             text NOT NULL DEFAULT '',
    input_tokens      bigint NOT NULL DEFAULT 0,
    output_tokens     bigint NOT NULL DEFAULT 0,
    cost_usd          numeric(14, 6) NOT NULL DEFAULT 0,
    cached            boolean NOT NULL DEFAULT false,
    feedback          qa_feedback,
    feedback_comment  text NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX qa_messages_thread_idx ON qa_messages (thread_id, created_at);

-- Keyed by sha256(normalized question + scope + index version): any chunk write bumps the index version,
-- so stale answers are never served; rows expire after 24h.
CREATE TABLE answer_cache (
    key         text PRIMARY KEY,
    answer      text NOT NULL,
    citations   jsonb NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL DEFAULT now(),
    hits        bigint NOT NULL DEFAULT 0
);
CREATE INDEX answer_cache_created_idx ON answer_cache (created_at);

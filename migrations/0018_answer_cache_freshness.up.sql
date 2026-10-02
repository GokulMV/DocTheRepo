-- Cached answers stay valid until something they rely on changes, instead of until any write anywhere:
-- an answer records the chunks it cites and their scopes (repository or space), and is stale once a cited
-- chunk is gone or any chunk in those scopes changed after the answer was cached.
ALTER TABLE answer_cache ADD COLUMN chunk_ids text[] NOT NULL DEFAULT '{}';
ALTER TABLE answer_cache ADD COLUMN scopes text[] NOT NULL DEFAULT '{}';
-- Old entries were keyed by the global index version; they cannot be checked, so drop them.
DELETE FROM answer_cache;
CREATE INDEX chunks_scope_updated_idx ON chunks (scope, updated_at);

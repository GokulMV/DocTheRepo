DROP INDEX IF EXISTS chunks_scope_updated_idx;
ALTER TABLE answer_cache DROP COLUMN IF EXISTS scopes;
ALTER TABLE answer_cache DROP COLUMN IF EXISTS chunk_ids;

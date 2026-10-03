DROP INDEX IF EXISTS answer_cache_scope_idx;
ALTER TABLE answer_cache DROP COLUMN embedding;
ALTER TABLE answer_cache DROP COLUMN embed_model;
ALTER TABLE answer_cache DROP COLUMN scope_key;
ALTER TABLE answer_cache DROP COLUMN question;

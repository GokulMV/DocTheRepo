-- A reworded question can reuse a cached answer: answers keep the question, its embedding and the scope it
-- was asked in. Embeddings are compared in the Hub (a few hundred fresh answers per scope at most), so
-- this needs no vector extension and works with any embedding size.
ALTER TABLE answer_cache ADD COLUMN question text NOT NULL DEFAULT '';
ALTER TABLE answer_cache ADD COLUMN scope_key text NOT NULL DEFAULT '';
ALTER TABLE answer_cache ADD COLUMN embed_model text NOT NULL DEFAULT '';
ALTER TABLE answer_cache ADD COLUMN embedding real[];
CREATE INDEX answer_cache_scope_idx ON answer_cache (scope_key, embed_model, created_at DESC) WHERE embedding IS NOT NULL;

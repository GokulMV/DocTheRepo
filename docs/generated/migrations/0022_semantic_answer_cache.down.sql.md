<!-- dth:generated source="migrations/0022_semantic_answer_cache.down.sql" — edit only inside dth:human blocks -->
# `migrations/0022_semantic_answer_cache.down.sql`

<!-- dth:chunk 96499dae9737b34e -->
## `migrations/0022_semantic_answer_cache.down.sql`

Removes the semantic answer cache enhancements from the database schema, reverting migration 0022. Drops the `answer_cache_scope_idx` index and removes columns (`embedding`, `embed_model`, `scope_key`, `question`) that were added to support semantic search and caching of answer-question pairs across different scope contexts.

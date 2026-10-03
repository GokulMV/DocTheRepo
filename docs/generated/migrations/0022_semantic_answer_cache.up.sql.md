<!-- dth:generated source="migrations/0022_semantic_answer_cache.up.sql" — edit only inside dth:human blocks -->
# `migrations/0022_semantic_answer_cache.up.sql`

<!-- dth:chunk 6b087f7c523d3e37 -->
## `migrations/0022_semantic_answer_cache.up.sql`

This migration extends the `answer_cache` table to support semantic answer reuse by adding columns for the original question text, scope context, embedding model name, and a vector embedding. A sparse index on scope, model, and recency filters to only cached answers with embeddings, enabling the system to find semantically similar questions within a specific scope without requiring a vector extension—the Hub compares embeddings in application code instead.

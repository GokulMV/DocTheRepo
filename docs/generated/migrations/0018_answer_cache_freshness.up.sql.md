<!-- dth:generated source="migrations/0018_answer_cache_freshness.up.sql" — edit only inside dth:human blocks -->
# `migrations/0018_answer_cache_freshness.up.sql`

<!-- dth:chunk 23422b6f6fcdc128 -->
## `migrations/0018_answer_cache_freshness.up.sql`

Migration to add cache invalidation tracking to the `answer_cache` table. Adds two columns to record which chunks an answer cites (`chunk_ids`) and their scopes (`scopes`), allowing cached answers to be invalidated only when those specific chunks or their scopes change, rather than on any database write. Clears existing cache entries since they cannot be validated against the new schema, and creates an index on `chunks(scope, updated_at)` to efficiently check if cached chunks have been modified.

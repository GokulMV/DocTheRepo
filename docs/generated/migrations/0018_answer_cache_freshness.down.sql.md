<!-- dth:generated source="migrations/0018_answer_cache_freshness.down.sql" — edit only inside dth:human blocks -->
# `migrations/0018_answer_cache_freshness.down.sql`

<!-- dth:chunk da71a3edf860467a -->
## `migrations/0018_answer_cache_freshness.down.sql`

This migration down-script reverts changes from the corresponding up-migration by removing the `scopes` and `chunk_ids` columns added to the `answer_cache` table, and dropping the `chunks_scope_updated_idx` index. The `IF EXISTS` clauses ensure idempotent execution without errors if the objects were already removed.

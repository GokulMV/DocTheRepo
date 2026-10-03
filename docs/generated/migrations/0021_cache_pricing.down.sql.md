<!-- dth:generated source="migrations/0021_cache_pricing.down.sql" — edit only inside dth:human blocks -->
# `migrations/0021_cache_pricing.down.sql`

<!-- dth:chunk ba1b3f1fa7fefa7e -->
## `migrations/0021_cache_pricing.down.sql`

Rollback migration that removes cache pricing and usage tracking columns. Reverts the forward migration by dropping the `investigated` column from `qa_messages`, removing `cache_write_per_mtok_usd` and `cache_read_per_mtok_usd` pricing columns from `cost_table`, and removing `cache_write_tokens` and `cache_read_tokens` tracking columns from `usage_events`. Used to undo schema changes related to prompt caching feature.

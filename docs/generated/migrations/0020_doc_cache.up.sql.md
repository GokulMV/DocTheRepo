<!-- dth:generated source="migrations/0020_doc_cache.up.sql" — edit only inside dth:human blocks -->
# `migrations/0020_doc_cache.up.sql`

<!-- dth:chunk 633076675632153f -->
## `migrations/0020_doc_cache.up.sql`

This migration creates a `doc_cache` table to store generated documentation keyed by content hash and symbol, enabling reuse of documentation for identical code across retried jobs, reverted changes, moved functions, or different repositories. It includes a `used_at` index for cache eviction strategies. The migration also extends the `savings_kind` enum with `'doc_reused'` and `'doc_no_call'` values to track documentation sourced from cache or code comments, adds `'docgen_fast'` to the `llm_feature` enum for optional cheaper model routing, and creates an `app_settings` table for hub-wide configuration stored as key-value pairs.

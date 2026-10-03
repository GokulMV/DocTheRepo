<!-- dth:generated source="migrations/0020_doc_cache.down.sql" — edit only inside dth:human blocks -->
# `migrations/0020_doc_cache.down.sql`

<!-- dth:chunk 09564e776b48f57b -->
## `migrations/0020_doc_cache.down.sql`

This migration reversal script drops the `app_settings` and `doc_cache` tables that were created in the forward migration. It notes that enum values (`doc_reused` and `doc_no_call`) cannot be removed from the database and therefore persist after rollback, which is a PostgreSQL limitation where enum values are immutable once added.

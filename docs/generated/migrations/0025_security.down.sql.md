<!-- dth:generated source="migrations/0025_security.down.sql" — edit only inside dth:human blocks -->
# `migrations/0025_security.down.sql`

<!-- dth:chunk ac37292d23d317dc -->
## `migrations/0025_security.down.sql`

Rollback migration that removes three security-related database tables created in the forward migration. Drops `security_module_cache`, `security_findings`, and `security_scans` tables if they exist, while intentionally preserving the `security` enum value in the `llm_feature` column since PostgreSQL does not support removing enum values.

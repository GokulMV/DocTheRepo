<!-- dth:generated source="migrations/0029_repo_docs.down.sql" — edit only inside dth:human blocks -->
# `migrations/0029_repo_docs.down.sql`

Database migration down script that removes the file_cards and repo_docs tables.

<!-- dth:chunk 23fbe674f48313b8 -->
## `migrations/0029_repo_docs.down.sql`

Rolls back a migration that created the `file_cards` and `repo_docs` tables, removing both tables from the database if they exist. This down migration allows reverting the schema changes introduced in the corresponding up migration (0029_repo_docs.up.sql).

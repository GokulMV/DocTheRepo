<!-- dth:generated source="migrations/0032_repo_docs_draft_problems.down.sql" — edit only inside dth:human blocks -->
# `migrations/0032_repo_docs_draft_problems.down.sql`

Database migration that reverses the addition of a `draft_problems` column to the `repo_docs` table.

<!-- dth:chunk 5f3c9cc4163fb637 -->
## `migrations/0032_repo_docs_draft_problems.down.sql`

Removes the `draft_problems` column from the `repo_docs` table, rolling back the schema change introduced by the corresponding up migration. This down migration is used when reverting the database to a previous state.

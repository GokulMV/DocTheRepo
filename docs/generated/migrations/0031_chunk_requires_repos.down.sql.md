<!-- dth:generated source="migrations/0031_chunk_requires_repos.down.sql" — edit only inside dth:human blocks -->
# `migrations/0031_chunk_requires_repos.down.sql`

Rollback migration that removes the requires_repos column from the chunks table.

<!-- dth:chunk 23930479102aaef8 -->
## `migrations/0031_chunk_requires_repos.down.sql`

Rollback migration that removes the `requires_repos` column from the `chunks` table. This migration is executed when rolling back the forward migration that added this column to store repository dependency information for chunks.

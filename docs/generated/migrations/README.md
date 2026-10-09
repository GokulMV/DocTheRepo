<!-- dth:generated index — edit only inside dth:human blocks -->
# `migrations`

- [`0028_mcp_servers.down.sql`](0028_mcp_servers.down.sql.md) — Down migration that removes the mcp_servers table when rolling back migration 0028.
- [`0028_mcp_servers.up.sql`](0028_mcp_servers.up.sql.md) — Database migration creating the mcp_servers table for managing external MCP server connections with authentication and tool configuration.
- [`0029_repo_docs.down.sql`](0029_repo_docs.down.sql.md) — Database migration down script that removes the file_cards and repo_docs tables.
- [`0029_repo_docs.up.sql`](0029_repo_docs.up.sql.md) — Migration to create repo_docs and file_cards tables for storing generated repository documentation with source tracking and confidence metrics.
- [`0030_docs_phase2.down.sql`](0030_docs_phase2.down.sql.md) — SQL downward migration that reverses phase 2 of documentation schema changes by removing an index, deleting null rows, restoring constraints, and dropping a ...
- [`0030_docs_phase2.up.sql`](0030_docs_phase2.up.sql.md)
- [`0031_chunk_requires_repos.down.sql`](0031_chunk_requires_repos.down.sql.md) — Rollback migration that removes the requires_repos column from the chunks table.
- [`0031_chunk_requires_repos.up.sql`](0031_chunk_requires_repos.up.sql.md)
- [`0032_repo_docs_draft_problems.down.sql`](0032_repo_docs_draft_problems.down.sql.md) — Database migration that reverses the addition of a `draft_problems` column to the `repo_docs` table.
- [`0032_repo_docs_draft_problems.up.sql`](0032_repo_docs_draft_problems.up.sql.md)

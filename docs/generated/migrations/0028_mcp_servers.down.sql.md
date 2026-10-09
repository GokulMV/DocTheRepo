<!-- dth:generated source="migrations/0028_mcp_servers.down.sql" — edit only inside dth:human blocks -->
# `migrations/0028_mcp_servers.down.sql`

Down migration that removes the mcp_servers table when rolling back migration 0028.

<!-- dth:chunk 973a7e18f1afd7dc -->
## `migrations/0028_mcp_servers.down.sql`

This down migration removes the `mcp_servers` table if it exists. It is the inverse of migration 0028, used to rollback the creation of the MCP (Model Context Protocol) servers table during database migration reversals.

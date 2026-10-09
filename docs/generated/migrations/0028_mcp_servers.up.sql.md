<!-- dth:generated source="migrations/0028_mcp_servers.up.sql" — edit only inside dth:human blocks -->
# `migrations/0028_mcp_servers.up.sql`

Database migration creating the mcp_servers table for managing external MCP server connections with authentication and tool configuration.

<!-- dth:chunk 24fa9da19b74244c -->
## `migrations/0028_mcp_servers.up.sql`

Creates the `mcp_servers` table to store configurations for external MCP (Model Context Protocol) servers that the Hub can call while processing requests. Each server has a unique name, URL, and authentication method (none, bearer token, header, OAuth, AWS, or Google). The table stores non-secret settings in `config` (JSONB), encrypted secrets in `secret_ciphertext`, and OAuth credentials in `oauth_ciphertext`. Access is controlled by `min_role` (viewer through owner). The `tools` column tracks available tools from the server, `tool_choices` stores admin preferences per tool, and `status` monitors health (new, ok, needs_sign_in, error). Timestamps track creation and updates.

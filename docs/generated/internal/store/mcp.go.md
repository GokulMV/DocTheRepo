<!-- dth:generated source="internal/store/mcp.go" — edit only inside dth:human blocks -->
# `internal/store/mcp.go`

Provides persistent storage and management of MCP (Model Context Protocol) server connections with encrypted secrets and OAuth tokens.

<!-- dth:chunk 7a2269925949e1b5 -->
## `MCPTool`

Represents a tool offered by an MCP server. The `ReadOnly` field indicates whether the tool mutates state (derived from the server's readOnlyHint). `InputSchema` holds the JSON-serialized tool invocation parameters.

<!-- dth:chunk 072c63a4a86d6ff1 -->
## `MCPServer`

Represents an MCP server connection without decrypted secrets. The `Auth` field specifies the authentication method (none, bearer, header, oauth, aws, google). `SignedIn` indicates whether an OAuth token is stored. `Status` tracks the connection state (new, ok, needs_sign_in, error). `Tools` lists the server's capabilities, and `ToolChoices` records which tools the admin has enabled for Hub use.

<!-- dth:chunk 0821c88c2a03ee31 -->
## `MCPServer.ToolOn`

Determines whether a tool may be invoked by the Hub: returns the admin's explicit choice if set, otherwise defaults to allowing only read-only tools.

<!-- dth:chunk da0534eea740436c -->
## `MCPServers`

Manages MCP server connections in the database, handling encryption and decryption of secrets and OAuth tokens. `SecretAAD` and `OAuthAAD` generate additional authenticated data for each operation to bind encryption to specific server IDs.

<!-- dth:chunk 8dcad64fcb5195ac -->
## `NewMCPServers`

NewMCPServers returns the MCP connection store.

<!-- dth:chunk 5a4abaffec632d3c -->
## `NewMCPServer`

Input parameters for creating a new MCP server connection. `Secret` is an optional authentication credential that will be encrypted before storage.

<!-- dth:chunk b40e4e797562c06c -->
## `MCPServers.Create`

Inserts a new MCP server connection with generated ID. If a secret is provided, it encrypts and stores it using the configured sealer; defaults auth to "none" and min_role to "editor". Returns the generated ID or an error.

<!-- dth:chunk 6a74eacd627ad45a -->
## `scanMCP`

Scans a database row into an MCPServer, unmarshaling JSON fields (config, tools, tool_choices) and setting defaults to empty collections. Derives `SignedIn` from OAuth presence, status, and auth type. Returns `ErrNotFound` if the row doesn't exist.

<!-- dth:chunk 968359eb60755d07 -->
## `MCPServers.List`

Retrieves all MCP server connections from the database, ordered by name.

<!-- dth:chunk 0f9b4f6109897f2e -->
## `MCPServers.Get`

Get returns one connection.

<!-- dth:chunk 1ce4c14f91404266 -->
## `MCPPatch`

Patch structure for updating an MCP server connection. Nil fields are skipped; `Secret` set to empty string removes the stored credential.

<!-- dth:chunk 9199162097571ea3 -->
## `MCPServers.Update`

Applies updates to an MCP server connection in a transaction. Changing URL or updating the secret resets the connection status to "new" and clears stored tools and OAuth tokens (which belong to the previous configuration). Each non-nil patch field is conditionally applied, and `updated_at` is refreshed.

<!-- dth:chunk 199a0b8e610f491b -->
## `MCPServers.Delete`

Removes an MCP server connection by ID. Returns `ErrNotFound` if no rows were deleted.

<!-- dth:chunk 7a05c40c01978a00 -->
## `MCPServers.Secret`

Decrypts and returns the stored secret credential for an MCP server, or an empty string if none is stored. Returns `ErrNotFound` if the server does not exist.

<!-- dth:chunk ae2590427830c37c -->
## `MCPServers.OAuth`

Decrypts and unmarshals the stored OAuth token record into the provided output variable. Returns true if a token exists, false if none is stored, or an error on failure. Returns `ErrNotFound` if the server does not exist.

<!-- dth:chunk bea3154e127a552a -->
## `MCPServers.SetOAuth`

Encrypts and stores an OAuth token record; passing nil removes the stored token. Updates the `updated_at` timestamp on success.

<!-- dth:chunk 980c4dc764d50bcc -->
## `MCPServers.SetStatus`

Records the outcome of a connection attempt: status, error message, and optionally the discovered tools. If tools is provided, it replaces the stored tools list; otherwise tools remain unchanged. Updates `checked_at` timestamp.

<!-- dth:chunk 92f4e5f248648c7d -->
## `__module__`

SQL column list for selecting MCP server data, including boolean expressions to check for encrypted secret and OAuth data presence (avoiding exposure of ciphertext).

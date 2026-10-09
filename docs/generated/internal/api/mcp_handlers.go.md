<!-- dth:generated source="internal/api/mcp_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/mcp_handlers.go`

This file implements HTTP handlers for managing MCP server connections through a REST API, including CRUD operations and OAuth sign-in flow for Hub administrators.

<!-- dth:chunk b3629d277c7eaddc -->
## `MCPDeps`

Bundles dependencies for managing MCP server connections. The `Auth` service handles audit logging, `Store` persists server configurations, `Manager` orchestrates active connections, and `SealKeys` encrypts secrets. Set `RequireSealed` to enforce encryption of secret values.

<!-- dth:chunk 64c505f8443f4cea -->
## `mcpHandlers`

Embeds MCPDeps to provide handler methods access to services for MCP server operations.

<!-- dth:chunk 248d34f7508e2366 -->
## `MCPRoutes`

Mounts MCP server management routes at `/mcp/*` restricted to admin users. Endpoints handle listing, creating, patching, deleting, and checking MCP servers, plus OAuth sign-in flow with callback handling.

<!-- dth:chunk 50bb9eaaeff5c3b6 -->
## `mcpHandlers.audit`

Logs an audit event for MCP server operations if an Auth service is available. Captures the action, resource type, ID, optional details, and client IP address.

<!-- dth:chunk c63b1400eacfd0d0 -->
## `validMCPURL`

Validates that a URL is a valid HTTP(S) address for an MCP server. Rejects HTTP URLs outside private networks (localhost, `.internal`, `.svc` domains), and blocks known cloud metadata endpoints. Returns a validation error if invalid.

<!-- dth:chunk a296f3e9df46ed95 -->
## `validRole`

Checks if a role string is either empty or one of the recognized roles (viewer, editor, admin, owner).

<!-- dth:chunk 1576244674fb05b2 -->
## `mcpHandlers.list`

Returns all MCP servers and the OAuth redirect URI for the requesting origin. On error, writes an error response.

<!-- dth:chunk f565e1a4fa58fb00 -->
## `mcpHandlers.create`

Creates a new MCP server with name, URL, authentication type, and optional configuration. Validates inputs, unseals the secret, stores the server, and initiates connection immediately (or marks as needing sign-in for OAuth). Returns the created server or a conflict error if the name is taken.

<!-- dth:chunk 2b039da904b80f7e -->
## `mcpHandlers.afterChange`

Fetches a server and initiates connection, unless it's disabled or an unsigned-in OAuth server (marked as needing sign-in instead). Clears any cached connection state before reconnecting.

<!-- dth:chunk 3b2d77b69cb21540 -->
## `mcpHandlers.patch`

Updates an MCP server's URL, role requirements, secrets, or enabled state. Validates URL and role, unseals secrets, persists changes, and reconnects immediately if URL, secrets, config, or enable flag changed. Returns the updated server.

<!-- dth:chunk f840699eecc6e9ce -->
## `mcpHandlers.remove`

Deletes an MCP server and clears its cached connection state, then logs the deletion.

<!-- dth:chunk 3869a6c8fd76d797 -->
## `mcpHandlers.check`

Tests the connection to an MCP server and returns its current status.

<!-- dth:chunk 182e6d151ae4219b -->
## `mcpHandlers.signIn`

Initiates OAuth sign-in for an MCP server by starting the sign-in flow and returning the provider's authorization URL. Returns ErrNotFound if the server doesn't exist, or a validation error if sign-in setup fails.

<!-- dth:chunk 517f5ea474db908c -->
## `mcpHandlers.callback`

Handles the OAuth provider's callback by exchanging state and code for sign-in completion. Redirects to `/connectors` with query parameters for success (mcp server ID) or error details. Logs successful sign-in as an audit event.

<!-- dth:chunk 550a337bd7847cb2 -->
## `requestOrigin`

Reconstructs the origin URL (scheme and host) from which the browser reached the Hub, accounting for TLS presence and X-Forwarded headers from proxies.

<!-- dth:chunk d447914239c8cbec -->
## `__module__`

Allowed authentication types for MCP servers: none, bearer token, custom header, OAuth, AWS signature, and Google authentication.

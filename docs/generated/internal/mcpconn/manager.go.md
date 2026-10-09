<!-- dth:generated source="internal/mcpconn/manager.go" — edit only inside dth:human blocks -->
# `internal/mcpconn/manager.go`

This file implements the Manager type that orchestrates connections to MCP servers, handling authentication (including OAuth flows), client lifecycle management, tool discovery and execution, and access control.

<!-- dth:chunk 1cc2494378e6fc6a -->
## `Store`

Defines the storage interface the Manager depends on for MCP server configurations, secrets, OAuth state, and connection status updates.

<!-- dth:chunk 641cf0d469187698 -->
## `Manager`

Manages connections to MCP servers. It maintains cached client connections indexed by server ID, handles OAuth flows, authenticates to servers using various methods, lists available tools, and executes tool calls with access control checks.

<!-- dth:chunk 33704e112e279c38 -->
## `conn`

Wraps an MCP client and stores its configuration fingerprint to detect when the underlying server settings have changed.

<!-- dth:chunk 15d1f42d989967d5 -->
## `Manager.httpClient`

Returns the configured HTTP client or creates a default one with a 60-second timeout.

<!-- dth:chunk 9c0cf4a6c25757af -->
## `Manager.RedirectURI`

Constructs the OAuth callback URL, using the configured public URL if available, otherwise the provided base address, combined with the standard callback path.

<!-- dth:chunk e8558aed984a8a76 -->
## `fingerprint`

Computes a JSON-based fingerprint of server configuration (URL, authentication method, settings, secret presence, sign-in state) to detect when cached connections need rebuilding.

<!-- dth:chunk 6d217a7e58b59a6f -->
## `Manager.Forget`

Removes a cached connection for a server, forcing it to be recreated on next use (typically after configuration changes).

<!-- dth:chunk 709b709c16b644b1 -->
## `Manager.client`

Retrieves or creates an MCP client for a server. It reuses cached clients when their configuration fingerprint matches, extracts custom headers from config entries prefixed with "header:", and sets the client name and version.

<!-- dth:chunk e50d31f9c7a1754b -->
## `Manager.authorizer`

Constructs the appropriate authorizer for a server based on its authentication method: bearer token, custom header, AWS SigV4, Google service account, or OAuth. Returns an error if required credentials are missing or the auth type is unknown.

<!-- dth:chunk 5c4b629659610682 -->
## `guessAWS`

Extracts AWS service and region from an endpoint hostname matching the pattern `service.region.api.aws`, preserving any values already explicitly configured.

<!-- dth:chunk 5c904e4dba4a5a4b -->
## `Manager.accessToken`

Retrieves a valid OAuth access token, refreshing if needed. Returns `ErrNeedsSignIn` if no token is stored or refresh fails with sign-in expiry; on refresh expiry, clears stored tokens, updates status, and clears the cached connection.

<!-- dth:chunk 9ed1cc8dd6c26ea8 -->
## `Manager.Check`

Connects to a server, lists its tools, and updates the stored status (ok, error, or needs_sign_in based on the outcome). Returns the refreshed server record.

<!-- dth:chunk cafd1276e8713f56 -->
## `Manager.listTools`

Fetches the list of tools from a server with a 45-second timeout, normalizes their titles from either `Title` or `Annotations.Title`, and returns them sorted by name.

<!-- dth:chunk 13df02d81ee73676 -->
## `explain`

Converts connection errors into human-readable explanations for admins. For 401 errors, provides specific guidance based on the authentication method; otherwise returns the error message.

<!-- dth:chunk 8591f3d8b9182654 -->
## `Manager.StartSignIn`

Begins OAuth sign-in by discovering the OAuth provider from the server, optionally using existing credentials or registering a new client. Returns the authorization URL. Requires https or localhost/IP addresses for the redirect URI. Validates that the connection uses OAuth authentication.

<!-- dth:chunk c8e8d31023769a5c -->
## `Manager.FinishSignIn`

Completes the OAuth sign-in callback, validating the state token using constant-time comparison and extracting the server ID. Returns the connection ID on success; runs a Check to verify the connection now works.

<!-- dth:chunk bc554d40c6ac0e20 -->
## `constantEq`

Compares two strings for equality using constant-time logic (XOR all bytes) to prevent timing attacks on security-sensitive values.

<!-- dth:chunk 4c3022365fe60dd7 -->
## `randomToken`

Generates a 24-byte random token encoded as URL-safe base64, used for OAuth state values.

<!-- dth:chunk 9943da3e2b3b0f72 -->
## `Tool`

Represents a tool available for execution, identifying it by a unique name combining the server slug and tool name, along with metadata and schema for invocation.

<!-- dth:chunk 20d1884623493cbb -->
## `slug`

Converts a string to a lowercase slug with non-alphanumeric characters replaced by underscores and leading/trailing underscores trimmed.

<!-- dth:chunk f326be64744c6326 -->
## `Manager.Tools`

Lists tools available to a user with a specific role: filters to enabled, connected servers where the user meets minimum role requirements, and includes only tools marked as enabled for that user.

<!-- dth:chunk 43341a4860492a2a -->
## `Manager.Call`

Executes a tool by name after verifying access and role permissions. Uses a configurable timeout (default 45 seconds), clips error output to 400 characters, and returns the tool reference and result text.

<!-- dth:chunk 0605805c23abba6c -->
## `firstNonEmpty`

Returns the first non-empty string from a list, after trimming whitespace, or the empty string if all are empty.

<!-- dth:chunk 85169a38e4bb458e -->
## `clip`

Truncates a string to n characters, appending an ellipsis if longer.

<!-- dth:chunk f6853eb279ce4fa4 -->
## `__module__`

Module-level constants and variables: `CallbackPath` for OAuth callbacks, `awsHost` regex to parse AWS endpoints, `ErrNeedsSignIn` error for missing OAuth credentials, and `slugRe` regex for slug generation.

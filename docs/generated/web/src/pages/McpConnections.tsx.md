<!-- dth:generated source="web/src/pages/McpConnections.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/McpConnections.tsx`

Page component for managing MCP (Model Context Protocol) server connections in the Ask Hub, including browsing available servers, adding new connections, and configuring tool access.

<!-- dth:chunk 0046a716c41a82b7 -->
## `useMcpServers`

Fetches the list of connected MCP servers and their OAuth redirect URI from the API using react-query.

<!-- dth:chunk ad604bee5689f7c7 -->
## `McpSection`

Main section component displaying available MCP catalog entries as tiles, connected servers in a table, and handling sign-in/error status messages. Shows a filtered or complete catalog view, manages modals for connecting new servers and editing tool settings, and displays query results or errors.

<!-- dth:chunk 8a48e9ed418e70a4 -->
## `toolOn`

Returns whether a specific tool is enabled for an MCP server: enabled if explicitly set in `tool_choices`, otherwise defaults to the tool's `read_only` status.

<!-- dth:chunk 78183c273a6df8b3 -->
## `McpTable`

Renders a table of connected MCP servers with actions like sign-in, check connection status, enable/disable, tool management, and deletion. Handles async operations via `useInvalidating` and displays status badges, error messages, and available tool counts.

<!-- dth:chunk 520b7a47a7e8cde1 -->
## `fill`

Replaces template placeholders like `{key}` in a URL with values from a record, trimming whitespace and trailing slashes from values. Leaves unreplaced placeholders unchanged.

<!-- dth:chunk 040ca6d5eb5deb60 -->
## `uniqueName`

Returns a unique name by returning the base string if not in the taken list, otherwise appending a numeric suffix (starting at 2) until a unique name is found.

<!-- dth:chunk 064214686643919c -->
## `ConnectMcp`

Dialog for adding a new MCP server connection. Handles configurable server address, authentication method (OAuth, bearer token, header, AWS IAM, Google service account), access control role, and optional OAuth client credentials. Validates form inputs, seals credentials before sending to API, and redirects to OAuth flow if needed.

<!-- dth:chunk 59c606eb633042f5 -->
## `RedirectHint`

Displays the OAuth redirect URI from the server query data if available, formatted as a code snippet for user reference.

<!-- dth:chunk 4aafc22e8b1ed20f -->
## `capital`

Capitalizes the first character of a string.

<!-- dth:chunk eae2285456ff265a -->
## `McpTools`

Dialog for toggling individual MCP server tools on/off. Tracks initial state, displays tool descriptions and read-only badges, warns when write-capable tools are enabled, and sends updates via PATCH API call.

<!-- dth:chunk c2b51c76b96045ce -->
## `__module__`

Module-level constants: `mcpKey` for query caching, `STATUS` map for server status display labels and tone colors, `ROLES` array for access control options, `POPULAR` list of popular MCP catalog keys shown by default, and re-export of `mcpEntry`.

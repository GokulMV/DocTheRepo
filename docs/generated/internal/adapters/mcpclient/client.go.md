<!-- dth:generated source="internal/adapters/mcpclient/client.go" — edit only inside dth:human blocks -->
# `internal/adapters/mcpclient/client.go`

Implements a client for communicating with Model Context Protocol (MCP) servers over HTTP, handling session management, JSON-RPC messaging, and tool invocation.

<!-- dth:chunk 4b5d505802483777 -->
## `Authorizer`

Authorizer adds credentials to a request; body is the request body (SigV4 signs it).

<!-- dth:chunk 4b9320f4812d6fa1 -->
## `AuthError`

A 401 Unauthorized or 403 Forbidden response from the server. The `WWWAuthenticate` header indicates where to obtain credentials, and `Body` contains the response body for additional context.

<!-- dth:chunk 64867ec935e70d4d -->
## `AuthError.Error`

Returns a human-readable error message: "the server refused access (403): the key or account lacks permission" for 403 status, or "the server needs sign-in (401)" for 401.

<!-- dth:chunk 231f96ce6e2f1720 -->
## `Tool`

Represents a tool available on the MCP server, with optional title, description, and JSON schema defining its input parameters. The `Annotations` field carries metadata about whether the tool is read-only or destructive.

<!-- dth:chunk 4e213d6122cb4dfe -->
## `Tool.ReadOnly`

ReadOnly reports whether the server marks the tool as not changing anything.

<!-- dth:chunk a5b2dce169343e40 -->
## `Result`

Result is a tool's output as text.

<!-- dth:chunk d86eecb7d478c87f -->
## `Client`

Represents a connection to one MCP server. It manages session state (automatically re-initializing when expired), tracks the negotiated protocol version, and uses atomic request ID generation. `Auth` is optional; `Headers` holds extra non-secret headers like `X-Grafana-URL`.

<!-- dth:chunk d1931555376c9460 -->
## `rpcError`

A JSON-RPC error object returned by the server, containing an error code, message, and optional structured data.

<!-- dth:chunk 701c36e58a371938 -->
## `rpcError.Error`

Formats the RPC error as "server error {code}: {message}".

<!-- dth:chunk 946b1484952687e6 -->
## `rpcResponse`

A JSON-RPC response object containing a request ID, result data, and optional error. Only one of `Result` or `Error` should be non-nil.

<!-- dth:chunk 376833d5ed5959f6 -->
## `Client.Initialize`

Explicitly initiates a new session with the server. Also called automatically on first use via `call()`.

<!-- dth:chunk f020ad5f173d6b19 -->
## `Client.initLocked`

Sends an `initialize` request with protocol version, capabilities, and client info, then confirms with an `initialized` notification. On success, stores the session ID and protocol version and marks the client as ready. Clears prior state on each call to ensure a fresh session.

<!-- dth:chunk 46366460828061b1 -->
## `Client.call`

Runs a JSON-RPC request, automatically initializing the session if needed. If a 404 is returned with an existing session, assumes the server forgot the session, reinitializes, and retries once.

<!-- dth:chunk 849bc8fbb52fe2a7 -->
## `Client.ListTools`

Fetches all tools from the server using pagination (up to 20 pages). Returns accumulated tools or an error on any page fetch failure.

<!-- dth:chunk 0906fcc581c3abfd -->
## `Client.CallTool`

Invokes a named tool with provided arguments and converts the response to plain text. Handles multiple content types: text is included directly; resources include their text or URI; resource links are formatted as "[link name: uri]". If no text content exists but structured content is present, includes that. Treats empty input as `{}`.

<!-- dth:chunk d6f97cf54054eddd -->
## `statusError`

An HTTP error with a status code and optional body text.

<!-- dth:chunk bfb7ab20513b69ed -->
## `statusError.Error`

Formats as "the server answered {status}: {body}" if body is present, otherwise just the status code.

<!-- dth:chunk 86159030ae73057a -->
## `Client.post`

Sends a JSON-RPC message (request or notification) with optional ID. Sets Content-Type, Accept headers, session and protocol headers if available, and applies authorization if configured. For requests, parses responses as JSON-RPC or SSE; for notifications, expects 202 and discards the body. Returns the session ID from response headers. Limits response body to `maxBody` bytes. Returns `AuthError` on 401/403, `statusError` on other 3xx/4xx/5xx codes, or decodes the result into `out`.

<!-- dth:chunk 3d69f5fc804e7832 -->
## `readStream`

Parses server-sent events (SSE) until receiving a JSON-RPC response matching the given request ID. Concatenates data from multi-line events (joined with newlines), ignores blank lines and unrelated messages, and returns the first matching response or an error if the stream closes without one. Uses a 64 KB scan buffer.

<!-- dth:chunk e8929be5d6bf04d1 -->
## `orStr`

Returns the string `s` if non-empty, otherwise returns the default string `def`.

<!-- dth:chunk 9ba8eff05b333d26 -->
## `__module__`

Declares the MCP protocol version and maximum body size (4 MB) limits for requests and responses.

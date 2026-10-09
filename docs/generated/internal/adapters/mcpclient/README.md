<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/adapters/mcpclient`

- [`auth.go`](auth.go.md) — Provides authentication strategies for MCP server requests: Bearer tokens, token-based functions (OAuth), AWS SigV4 signing, and Google service account authe...
- [`client.go`](client.go.md) — Implements a client for communicating with Model Context Protocol (MCP) servers over HTTP, handling session management, JSON-RPC messaging, and tool invocation.
- [`oauth.go`](oauth.go.md) — Implements OAuth 2.0 discovery, client registration, and authorization-code flow with PKCE for MCP server authentication.

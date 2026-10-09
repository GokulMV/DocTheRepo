<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/api`

- [`ask_handlers.go`](ask_handlers.go.md) — Defines HTTP handlers for answering questions using a RAG-backed AI engine with conversation history, streaming support, rate limiting, and repository access...
- [`auth_handlers.go`](auth_handlers.go.md) — This file implements HTTP request handlers for authentication operations including user profile retrieval, login, and credential management.
- [`mcp_handlers.go`](mcp_handlers.go.md) — This file implements HTTP handlers for managing MCP server connections through a REST API, including CRUD operations and OAuth sign-in flow for Hub administr...
- [`openapi.go`](openapi.go.md) — Defines the OpenAPI specification and catalog of REST API operations exposed by the Hub.
- [`ops_handlers.go`](ops_handlers.go.md) — Defines HTTP handlers for operations endpoints, including job management and analytics queries, with role-based access control.
- [`repodocs_handlers.go`](repodocs_handlers.go.md) — HTTP request handlers and routes for repository documentation operations, including retrieval, reporting, and administrative management.
- [`server.go`](server.go.md) — Defines the HTTP router and its dependency configuration, orchestrating endpoints for health checks, webhooks, authentication, and the web UI.

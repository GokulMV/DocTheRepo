<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/api`

- [`auth_handlers.go`](auth_handlers.go.md) — HTTP handlers for authentication endpoints including login, logout, SSO callbacks, user profile, and password management.
- [`openapi.go`](openapi.go.md) — Defines the complete OpenAPI endpoint registry and metadata for the DocTheRepo API hub.
- [`ops_handlers.go`](ops_handlers.go.md) — Defines HTTP handlers for operations endpoints, including job management and analytics queries, with role-based access control.
- [`server.go`](server.go.md) — Defines the Deps struct and NewRouter function that configure and build the HTTP handler tree for the Hub API server.

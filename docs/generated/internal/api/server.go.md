<!-- dth:generated source="internal/api/server.go" — edit only inside dth:human blocks -->
# `internal/api/server.go`

<!-- dth:chunk de778cad55a697ce -->
## `Deps`

Deps aggregates all collaborators needed to construct the HTTP router. It includes logging, metrics, health checks, webhook ingestion handlers (Git and signal sources), authentication services, UI serving, and optional features like OIDC SSO, email invitations, and secret policies. The WebhookPerMinute field defaults to 6000 when zero or negative. Nil values for Git, Signals, Auth, UI, Mailer, and Settings disable those subsystems; OIDC and SealKeys are nil when unused.

<!-- dth:chunk 3c1311225614c853 -->
## `NewRouter`

NewRouter builds the complete HTTP handler tree from dependencies, setting up middleware (security headers, correlation IDs, recovery, metrics instrumentation), health/readiness endpoints, webhook ingestion routes for Git and signal sources with per-connector rate limiting, authenticated /api/v1 routes with OpenAPI spec and configurable mount points, and a catch-all NotFound handler that either serves the single-page UI or returns JSON errors. The root router reference is retained to allow settings endpoints to call the API in-process after construction completes.

<!-- dth:generated source="internal/api/server.go" — edit only inside dth:human blocks -->
# `internal/api/server.go`

Defines the HTTP router and its dependency configuration, orchestrating endpoints for health checks, webhooks, authentication, and the web UI.

<!-- dth:chunk de778cad55a697ce -->
## `Deps`

Configuration struct for the HTTP router, containing dependencies for logging, metrics, health checks, webhook ingestion (Git and signal sources), authentication (Auth/OIDC), rate limiting, cookie security, additional API routes, UI serving, and feature flags. Rate limiting defaults to 6000 webhooks per minute per connector if not specified. Auth enables the full /api/v1 endpoints; without it, only health and ingress endpoints are available. OIDC enables single sign-on when configured. SealKeys with RequireSealed enforces encrypted browser secrets. UI serves the web app as a fallback for unmatched routes. HasIssues, HasSystem, and DocsV2 control feature visibility in the UI. Settings enables the /settings/apply endpoints when non-nil.

<!-- dth:chunk 3c1311225614c853 -->
## `NewRouter`

Constructs and returns the complete HTTP handler tree using chi routing. Sets up middleware for security headers, correlation IDs, panic recovery, and metrics instrumentation. Registers health (/healthz) and readiness (/readyz) endpoints. Conditionally mounts Git webhooks (/hooks/github|gitlab), signal webhooks (/hooks/{source} with special handling for Firehose), and authenticated API routes (/api/v1) with OpenAPI schema and pluggable route mounts. Configures OIDC from deployment config if Auth is present but OIDC not yet initialized. Routes unmatched requests to the UI handler if present, otherwise returns JSON 404 errors; /api and /hooks paths always return JSON errors. Validates webhook rate limit, defaulting to 6000 per minute per connector.

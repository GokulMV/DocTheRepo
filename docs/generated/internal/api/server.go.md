<!-- dth:generated source="internal/api/server.go" — edit only inside dth:human blocks -->
# `internal/api/server.go`

Defines the Deps struct and NewRouter function that configure and build the HTTP handler tree for the Hub API server.

<!-- dth:chunk de778cad55a697ce -->
## `Deps`

Defines the dependencies and configuration options needed to construct the HTTP router. Fields control feature availability (Auth enables `/api/v1`, Git/Signals enable webhook ingress, UI enables the web app), ingress behavior (WebhookPerMinute rate limiting, SealKeys for secret validation), and operational context (Mailer, HasIssues, Environment, OIDC, PublicURL). Later router initialization phases extend this struct to add additional handlers or middleware.

<!-- dth:chunk 3c1311225614c853 -->
## `NewRouter`

Constructs the complete HTTP handler tree using the provided dependencies. Configures core middleware (security headers, correlation, instrumentation), health endpoints (/healthz, /readyz), conditional webhook ingress routes for git and signals, authenticated API routes under /api/v1 with optional additional route groups from Deps.V1, and a catch-all that either serves the single-page app (if UI provided) or returns a JSON 404. Webhook rate limiting defaults to 6000 per minute if not specified.

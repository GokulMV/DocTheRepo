<!-- dth:generated source="internal/api/openapi.go" — edit only inside dth:human blocks -->
# `internal/api/openapi.go`

Defines the complete OpenAPI endpoint registry and metadata for the DocTheRepo API hub.

<!-- dth:chunk 8d7cd432e67ea5ec -->
## `__module__`

Defines the complete OpenAPI endpoint registry for the application as a static slice of operation metadata. Each entry specifies HTTP method, route path, category, description, required permission level, and flags for whether the endpoint expects request/response bodies. Covers webhook ingestion, authentication (SSO/password/tokens), user and organization management, documentation generation, AI queries (Ask), security scanning, library management, LLM provider configuration, GitHub App integration, architecture visualization, analytics, and issue/alert management. Also initializes sync-protected storage for lazy-loaded OpenAPI specification generation. This serves as the single source of truth for all API routes and their access control requirements.

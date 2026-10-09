<!-- dth:generated source="internal/api/openapi.go" — edit only inside dth:human blocks -->
# `internal/api/openapi.go`

Exposes a complete OpenAPI specification for the Hub's REST API, defining all routes, methods, authentication requirements, and their purposes.

<!-- dth:chunk 8d7cd432e67ea5ec -->
## `__module__`

The file contains a complete OpenAPI specification for a documentation platform API, exposed as a slice of operation structs defining HTTP method, path, category, description, required permission, whether the request/response are JSON, and whether the response streams.

Each endpoint entry specifies its semantics: ingestion webhooks (Git, Firehose, signals), authentication flows (SSO, tokens, invites), user and role management, Q&A threads, documentation generation and retrieval, security scanning, library uploads, repository tracking, LLM provider and connector management, settings, job queues, analytics, issue tracking, and health checks. The spec is built once on first access via the `specOnce` sync.Once guard and cached in `spec` for reuse.

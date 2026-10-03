<!-- dth:generated source="internal/api/openapi.go" — edit only inside dth:human blocks -->
# `internal/api/openapi.go`

<!-- dth:chunk 8d7cd432e67ea5ec -->
## `__module__`

A comprehensive list of all API operations the service exposes, each entry containing an HTTP method, endpoint path, category, human-readable description, required permission scope (empty if public), and flags indicating whether it requires authentication or streams responses. The operations span authentication, user management, documentation generation, Q&A threads, security scanning, library management, repository tracking, connector configuration, LLM provider setup, settings management, job monitoring, analytics, and issue tracking. Also declares a sync.Once guard and a cached spec map for lazy-loading the OpenAPI specification.

Clients can iterate `Operations` to generate API documentation or UI route listings. The `spec` variable and its `specOnce` guard enable thread-safe singleton initialization of the OpenAPI schema.

<!-- dth:generated source="internal/api/architecture_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/architecture_handlers.go`

<!-- dth:chunk 1e45dc55fcee7883 -->
## `ArchitectureRoutes`

Mounts HTTP routes for architecture endpoints, requiring `RoleViewer` for read access (`/architecture`, `/architecture/repos/{id}`, `/architecture/diagrams/{id}`) and `RoleEditor` for mutations (`POST /architecture/repos/{id}/scan`). The `repoAllowed` helper checks repository access via the request scope and returns a 404 for unauthorized access (hiding repository existence). Diagram responses include security headers (CSP, frame options, cache control). Scan operations invoke `d.Scan`, audit successful requests, and specially handle git host errors (returning 502 with detailed error info rather than generic retry messages to expose revoked tokens or permission issues).

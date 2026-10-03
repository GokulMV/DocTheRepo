<!-- dth:generated source="internal/api/security_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/security_handlers.go`

<!-- dth:chunk 9b538547c7c24fd4 -->
## `SecurityDeps`

Dependency container for security scanning and fixing operations. Provides access to authentication, scan storage, job queuing, and a planning function that estimates security scans without making model calls.

<!-- dth:chunk ee2ae7bec11598c6 -->
## `SecurityRoutes`

Mounts the `/security` route group with endpoints for querying available modules, listing scans, retrieving scan details, planning scans, initiating scans, and fixing findings via pull requests. All endpoints require editor role and check repository read permissions. Scan and fix operations enqueue jobs for async processing and audit user actions. The modules helper validates requested modules are static (code-only) and deduplicates them, defaulting to modules with `Default: true` when none are specified. Finding fixes accept 1–25 findings per request and return a conflict if none can be fixed.

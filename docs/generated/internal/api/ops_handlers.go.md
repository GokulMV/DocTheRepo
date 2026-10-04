<!-- dth:generated source="internal/api/ops_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/ops_handlers.go`

Defines HTTP handlers for operations endpoints, including job management and analytics queries, with role-based access control.

<!-- dth:chunk acdd2fec3a956038 -->
## `OpsRoutes`

Registers analytics and job management routes, mounting endpoints for jobs, activity, and analytics under the `/analytics` and `/jobs` prefixes. Routes are organized by role: `RoleViewer` can access list/get jobs, activity, usage, savings, sift, and pipeline analytics; `RoleAdmin` can additionally retry jobs and access connector analytics. The `timeRange` helper automatically parses optional `from` and `to` query parameters with a 30-day default window.

<!-- dth:chunk 656a2dfd541726c4 -->
## `opsHandlers.sift`

Handles HTTP GET requests to `/analytics/sift`, extracting a time range from query parameters (defaulting to 30 days) and delegating to the `Browse.Sift` method to retrieve analytics data. Returns the result as JSON or writes an error response if parsing or retrieval fails.

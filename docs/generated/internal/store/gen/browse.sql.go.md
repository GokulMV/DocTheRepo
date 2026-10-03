<!-- dth:generated source="internal/store/gen/browse.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/browse.sql.go`

<!-- dth:chunk 5719eb41c70450f9 -->
## `Queries.DocAncestors`

Returns a slice of node IDs from a repo node's root ancestors down to (but excluding) the given node, root first. Uses a recursive SQL query to traverse the parent hierarchy with a depth limit of 64 levels. Returns an error if the database query or row scanning fails.

<!-- dth:chunk de128976374cb063 -->
## `Queries.UpdateConnector`

Updates a connector's configuration fields (name, config, credentials, webhook settings, mode, poll interval, and enabled state) using coalesce in SQL to preserve existing values for any nil parameters. Returns the number of affected rows or an error. The comment indicates the upload connector is system-managed and excluded from typical connector management operations.

<!-- dth:chunk b6ae8e5b3e91162d -->
## `__module__`

SQL query constant definitions generated from sqlc, mapping named queries to their SQL statements. Includes queries for document node traversal, connector and shelf management, entity relationships, spend limits, and chunk token accounting. Notable queries: `docAncestors` uses recursive CTE for hierarchical traversal; `listConnectorsPublic` excludes 'upload' type connectors; `updateConnector` uses coalesce for optional field updates.

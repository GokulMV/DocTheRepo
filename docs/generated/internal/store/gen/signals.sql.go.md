<!-- dth:generated source="internal/store/gen/signals.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/signals.sql.go`

Generated database query methods for managing signals, issues, known issues, and code chunks.

<!-- dth:chunk eb6430218c976a35 -->
## `Queries.FrameChunk`

Executes the `FrameChunk` SQL query to find code chunks matching a stack frame. Given repository IDs, a file path, and function name (bare and qualified forms), returns up to one matching `Chunk` from the database, ranked by symbol preference and path specificity. Returns an error if the query fails or row scanning fails.

<!-- dth:chunk 9f95d5f7eb8c41cd -->
## `__module__`

SQL query constant strings used by the Queries methods. Includes queries for managing known issues (create, update, delete, list, get active), retrieving issue counts and samples, getting issues by fingerprint, resolving issues, decoding token estimates, and the `frameChunk` query for stack frame lookup. Each constant is tagged with its sqlc operation type (`:one`, `:many`, `:exec`, `:execrows`).

<!-- dth:generated source="internal/store/chunks.go" — edit only inside dth:human blocks -->
# `internal/store/chunks.go`

chunks.go provides database access and manipulation for code chunks, including upsert, delete, retrieval, and conversion operations.

<!-- dth:chunk c2c505ed27780218 -->
## `Chunks.Apply`

Apply writes a manifest update to the database atomically, processing upserts, revivals, soft deletes, and hard deletes in a single transaction, then bumps the index version for cache invalidation. It returns 0 and nil for empty writes; otherwise returns the new version number or an error if any operation fails.

<!-- dth:chunk 4bd9e110195b85b9 -->
## `toChunks`

toChunks converts a slice of database chunk rows to a slice of domain ports.Chunk objects, mapping all fields including handling the nullable RepoID by dereferencing it when present.

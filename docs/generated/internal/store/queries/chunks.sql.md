<!-- dth:generated source="internal/store/queries/chunks.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/chunks.sql`

Defines SQL queries for storing, retrieving, and managing lifecycle of indexed code chunks across repositories.

<!-- dth:chunk cbf9d48c65cd9595 -->
## `internal/store/queries/chunks.sql`

### Chunk Storage and Lifecycle Operations

This SQL file defines 14 queries managing stored code chunks across multiple repositories and sources:

**Retrieval queries** fetch chunks by paths, IDs, or repository scope: `ChunksForPaths` returns chunks for specific paths during push recomputation; `ChunksByIDs` retrieves chunks by ID array; `LiveChunks` provides keyset-paginated iteration of non-deleted chunks for reindexing and exports; `SharedChunksForPath` gets repo-less chunks (e.g., Confluence pages); `ChunksAtPathAnyRepo` retrieves generated documentation chunks at a path across all repos.

**Mutation queries** manage chunk lifecycle: `UpsertChunk` inserts or updates a chunk with updated_at timestamp and clears deletion marker; `SoftDeleteChunks` and `SoftDeleteSharedPath` mark chunks deleted without removal; `ReviveChunks` un-deletes chunks; `DeleteChunkIDs` permanently removes chunks; `GCChunks` garbage-collects soft-deleted chunks older than a cutoff.

**Index versioning** tracks changes: `BumpIndexVersion` increments version; `GetIndexVersion` reads current version, enabling efficient change detection.

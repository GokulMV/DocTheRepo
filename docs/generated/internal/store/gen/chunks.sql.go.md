<!-- dth:generated source="internal/store/gen/chunks.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/chunks.sql.go`

Generated database query methods and SQL constants for managing documentation chunks with support for soft-deletion, versioning, and multi-repository indexing.

<!-- dth:chunk b5bf7f13af7bc416 -->
## `Queries.ChunksAtPathAnyRepo`

Retrieves all stored chunks (both live and soft-deleted) at a given path across any repository, selecting only chunks with `source = 'generated_doc'`. Executes a database query and scans results into a slice of `Chunk` structs, returning any database or scan errors.

<!-- dth:chunk bcc7bf9c83a5b953 -->
## `Queries.ChunksByIDs`

Retrieves chunks by their IDs from a provided slice. Executes a database query using `chunk_id = ANY($1)` to select multiple chunks in a single operation, scanning results into a slice of `Chunk` structs. Returns any database or scan errors encountered.

<!-- dth:chunk 407ccc8c539ab72b -->
## `Queries.ChunksForPaths`

Retrieves all stored chunks (live and soft-deleted) for specific paths within a given repository and source, typically used when reprocessing files after a push. Scans results into a `Chunk` slice using the provided `ChunksForPathsParams` (repo ID, source, and path list). Returns any database or scan errors.

<!-- dth:chunk 5dc952434d93242c -->
## `Queries.LiveChunks`

Performs keyset-paginated iteration over live (non-deleted) chunks, ordered by chunk ID, for operations like reindexing or exports. Accepts `LiveChunksParams` specifying an `After` cursor and limit. Returns a slice of `Chunk` structs or any database/scan errors.

<!-- dth:chunk 00d79596784d06f5 -->
## `Queries.SharedChunksForPath`

Retrieves all stored chunks (live and soft-deleted) for a repo-less document (e.g., Confluence page or Jira issue) identified by source and path. Scans results into a `Chunk` slice using `SharedChunksForPathParams`. Returns any database or scan errors.

<!-- dth:chunk a023a5857e16c003 -->
## `UpsertChunkParams`

Parameters struct for upserting a single chunk, containing all chunk metadata: identifiers (chunk ID, optional repo ID), content data (scope, source, path, symbol, language, content with hash and signature), source control info (commit SHA, URL), and an array of required repository UUIDs for dependency tracking.

<!-- dth:chunk 38f5a6d817aef813 -->
## `Queries.UpsertChunk`

Inserts or updates a chunk in the database. On conflict by chunk ID, updates all fields and clears `deleted_at` (reviving soft-deleted chunks), with `updated_at` set to current time. Returns any database execution error.

<!-- dth:chunk 039fb7b441c317ab -->
## `__module__`

SQL query constants for chunk operations, including retrieval by path or ID, pagination of live chunks, and upsert/delete operations with soft-delete support for lifecycle management of documentation chunks.

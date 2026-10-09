<!-- dth:generated source="internal/ports/chunks.go" — edit only inside dth:human blocks -->
# `internal/ports/chunks.go`

Defines the Chunk type representing a retrieval unit of code or documentation with metadata, and ChunkSource constants for categorizing chunk origins.

<!-- dth:chunk d708e9aae5f0547a -->
## `Chunk`

Chunk represents a retrieval-sized, individually addressable unit of code or documentation. It contains the content itself along with metadata for identification (ID, scope, path, symbol), source tracking (Source, Language, CommitSHA), navigation (URL, line numbers), and access control via RequiresRepos, which specifies additional repositories required to view multi-repo content. The ContentHash and Signature support integrity verification and code identification. Timestamps track creation and soft deletion.

<!-- dth:chunk 1bbef12530937b3d -->
## `__module__`

Defines ChunkSource constants representing the origin of a chunk: SourceCode for source code, SourceGeneratedDoc for automatically generated documentation, SourceImportedDoc for imported documentation, SourceConfluence/SourceJira/SourceNotion for external knowledge bases, SourceUpload for user-uploaded content, SourceIssueDecode for parsed issue content, and SourceTool for MCP tool results retrieved dynamically during query answering but not persisted.

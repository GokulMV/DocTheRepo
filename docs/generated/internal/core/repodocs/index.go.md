<!-- dth:generated source="internal/core/repodocs/index.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/index.go`

This file provides functions to convert repository and system documentation into searchable chunks with metadata and access controls.

<!-- dth:chunk 916daa2ff7840758 -->
## `Chunks`

Converts a document into searchable chunks: a summary chunk and one chunk per section. Each chunk includes breadcrumb context (repository › document › section) and metadata like commit SHA (abbreviated), confidence level, path, and URL anchor, making it self-contained for search results. Returns nil if the document's status is not "ok". For module-type documents, the breadcrumb indicates "Module" in the title.

<!-- dth:chunk 99feb2c1f8e5637a -->
## `short`

Abbreviates a commit SHA to its first 7 characters if longer, otherwise returns it unchanged. Used to create readable commit references in chunk content.

<!-- dth:chunk f86515d684ad399b -->
## `SystemChunks`

Converts the System architecture document into searchable chunks with multi-repository access control. Each chunk requires read access to all provided repository IDs (sorted and stored in RequiresRepos), ensuring only authorized users can find system-level documentation. Returns nil if the document's status is not "ok" or if no repository IDs are provided. Like `Chunks`, it creates an "At a glance" summary chunk plus one per section.

<!-- dth:chunk 464c619fcbcc573f -->
## `__module__`

Constant path identifier used for system architecture documentation chunks, representing the logical location of system-level documentation in the search system.

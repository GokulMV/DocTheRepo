<!-- dth:generated source="internal/core/pipeline/repodocs.go" — edit only inside dth:human blocks -->
# `internal/core/pipeline/repodocs.go`

Handles documentation generation for repositories and system architecture by processing jobs, managing LLM-based doc generation with cost controls, and indexing results for search.

<!-- dth:chunk ee9de0dee06fce02 -->
## `RepoDocsPayload`

Payload structure for the `repo_docs` job that controls how repository documentation is generated. Supports full rewrites (`Full`), selective updates (`Only`), retrying failed documents (`RetryFailed`), and cost controls via `OverrideCeiling` to temporarily exceed spend limits. The `DryRun` flag estimates costs without writing.

<!-- dth:chunk 84b0b5d48aa64360 -->
## `RepoDocsFacts`

RepoDocsFacts reads facts and removes the per-file docs Docs v2 replaces.

<!-- dth:chunk e530d5ce45b92c9f -->
## `DocsBudget`

DocsBudget reports a repository's monthly docs cap and what this month's docs cost so far. estimate prices writing what is missing, for setting a first cap.

<!-- dth:chunk 3fba1aa14cf4f339 -->
## `skipPath`

Checks if a file path should be excluded from documentation processing based on common dependency and build directories (node_modules, vendor, dist, etc.). Returns true if the path starts with or contains a skip directory as a path component.

<!-- dth:chunk 89295343cfc1c07c -->
## `Pipeline.RepoDocs`

Handles the `repo_docs` job by generating documentation for a repository's files. Validates that Docs v2 is configured, retrieves the repository and its commit history, fetches special files (READMEs, etc.) within a size limit, and generates docs using an LLM. Checks cost budgets before writing, supports dry runs, and indexes generated documents for search. Queues a `system_docs` job if repository documents changed, affecting system architecture.

<!-- dth:chunk 28b1ff00ea305f78 -->
## `Pipeline.indexDocs`

Indexes generated repository documents into the search index by converting each doc into searchable chunks. Computes diff against stored chunks to determine additions, changes, and removals; applies chunk updates and removes legacy doc files. Embeddings are dropped on spend blocks or mismatches but text search remains available.

<!-- dth:chunk 1c80fe1328362c1d -->
## `SystemStore`

Interface for reading and storing system-level documentation. Retrieves links between tracked repositories, the system architecture document, per-repository documents, and chunks for search indexing. Supports deleting the system doc and storing updated docs.

<!-- dth:chunk e6f2dfca933b57b9 -->
## `SystemDocsPayload`

SystemDocsPayload is the system_docs job payload.

<!-- dth:chunk 21cc399259e66afd -->
## `Pipeline.queueSystemDocs`

Enqueues a `system_docs` job to regenerate the system architecture after a repository's documents change. Uses a serial key to ensure sequential processing and a dedup key to coalesce multiple requests into one job.

<!-- dth:chunk b53c95945de307ca -->
## `Pipeline.SystemDocs`

Handles the `system_docs` job by writing the system architecture when tracked repositories have links (APIs, events, shared packages) and removing it when they don't. Fetches each repository's overview and architecture docs, computes a content hash to detect changes, and calls the generator to write system docs. Updates the index and handles spend blocks by returning early while keeping the last valid version.

<!-- dth:chunk ccedaf3cf51651f1 -->
## `sortedIDs`

Returns repository IDs from a map, sorted by their name values (the map's values), used to ensure deterministic ordering when building system input from multiple repositories.

<!-- dth:chunk 26d650bede57781c -->
## `Pipeline.indexSystem`

Indexes the system architecture document into the search index as chunks, one per section, each tagged with all affected repository IDs. Removes the index entry if the doc is nil (system removed). Handles spend blocks and embedding mismatches by retaining text searchability.

<!-- dth:chunk 4e61ea5abd4729ea -->
## `__module__`

Module-level constants and errors: `ErrDocsBudget` signals when a repository's monthly documentation generation budget is exhausted; `maxSpecialBytes` caps special file content at 12KB before trimming; `skipDirs` lists directories excluded from documentation (node_modules, vendor, build artifacts, etc.).

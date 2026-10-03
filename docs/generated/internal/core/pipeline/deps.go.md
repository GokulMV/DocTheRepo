<!-- dth:generated source="internal/core/pipeline/deps.go" — edit only inside dth:human blocks -->
# `internal/core/pipeline/deps.go`

<!-- dth:chunk 2aecc5346c1ddc17 -->
## `DocsStore`

Interface for interacting with the documentation storage system. It manages persistent storage of generated documentation files, deletion of outdated docs, and querying which code chunks have been documented. Used by the pipeline to persist generated docs and track documentation state across runs.

<!-- dth:chunk 1e03486643496908 -->
## `DocCache`

DocCache holds generated docs by the code they describe (docrouter).

<!-- dth:chunk 9ef93207e6e2ac8e -->
## `Pipeline`

Container holding all dependencies and configuration needed by job handlers in the documentation pipeline. Includes stores for repos, code chunks, and dependency graphs; code generation services (docgen, indexing); a language model gateway; and optional callbacks for tracking file changes and job progress. DocGenParallel controls concurrency (default 4), DocCache and DocMode optimize generation cost and thoroughness, and ModeSetting allows runtime override of the documentation mode.

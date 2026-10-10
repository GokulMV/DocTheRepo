<!-- dth:generated source="internal/core/pipeline/codepush.go" — edit only inside dth:human blocks -->
# `internal/core/pipeline/codepush.go`

Handles code push events by loading and triaging changed files, generating documentation, updating architectural knowledge graphs, and landing documentation changes.

<!-- dth:chunk c6bde106ad1ffb60 -->
## `Pipeline.CodePush`

Processes a code push event by comparing repository state, triaging changed files, generating documentation, and landing doc changes as a pull request. Handles dry runs, forced paths, and full re-documentation. Aborts early if the repository is disabled or if changes are cosmetic. Calls `updateGraph` to track architecture entities before any documentation generation, and delegates to `codePushV2` when enabled. Records token savings for avoided work (reused docs, skipped triage). Returns structured outcome with documentation statistics, landing result, and estimated token usage.

<!-- dth:chunk 6c09f8cb7abb7393 -->
## `readGoModule`

Extracts and returns the module path from a repository's root `go.mod` file, or an empty string if the file cannot be read or parsed. Uses a regex to find the module declaration in the file content.

<!-- dth:chunk 57cecf9ac0ac77b8 -->
## `goModuleOf`

Reads the Go module path only when the repository contains Go files, avoiding unnecessary file fetches for non-Go repositories. Iterates through the file work to detect Go language files, then calls `readGoModule` if found.

<!-- dth:chunk e2aad50196a45691 -->
## `Pipeline.generate`

Routes documentation targets through decision logic (reuse cached docs, use code comments, fast route, or full generation), builds scoped context with imports for each file, and generates documentation in parallel. Avoids paid calls when docs are unchanged in full runs or when code has existing documentation. Caches generated docs for reuse on retries. Reports progress during generation and falls back to main model if fast route becomes unavailable mid-job.

<!-- dth:chunk efe015772d557586 -->
## `Pipeline.updateGraph`

Writes parsed code structures (endpoints, topics, datastores, imports, dependencies) to the knowledge graph for architectural views and downstream services. Runs before documentation generation so architecture reflects every push even when docs cannot be written. Handles file renames by clearing old source keys. If a service name is configured, records a deployment edge from the repository to the service.

<!-- dth:chunk 3747c01400994ffa -->
## `extractGraph`

Extracts architectural knowledge graph entities and relationships from a changed file by analyzing its code structure, imports, and special files (manifests, deployment configs, codeowners). Uses the Go module path to recognize internal package imports as repository edges.

<!-- dth:chunk a0bd7b807361b0b3 -->
## `Pipeline.codePushV2`

Indexes pushed code and enqueues repository documentation generation for Docs v2 (Hub-based). On dry runs, immediately queues a `RepoDocs` job. Otherwise, persists the indexed code and last-processed SHA, then enqueues a `JobRepoDocs` task with deduplication keyed to the commit SHA. The enqueue reason defaults to "push {short_sha}" if not provided. Logs indexed file and chunk counts.

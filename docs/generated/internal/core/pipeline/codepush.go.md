<!-- dth:generated source="internal/core/pipeline/codepush.go" — edit only inside dth:human blocks -->
# `internal/core/pipeline/codepush.go`

<!-- dth:chunk 8c19537f8516e5da -->
## `CodePushPayload`

Webhook/poller payload for a code_push job. Specifies the repository, commit range (Before/After), optional paths to force-regenerate docs for (e.g., after a stale PR closes), and flags for full regeneration, dry-run, and spend-ceiling overrides.

<!-- dth:chunk 6e3c9f8217680403 -->
## `CodePushResult`

Job result written to storage, containing the commit range processed, triage summary, counts of chunks added/changed/removed and files renamed, how many targets were documented, doc file paths, embedding counts, landing result if docs were pushed, estimated tokens, router tier breakdown (how targets were routed: unchanged, reuse, comment, fast, full), and notes.

<!-- dth:chunk c212afd2e290243b -->
## `fileWork`

One changed source file through the pipeline: stores its change metadata (added/modified/deleted), triage verdict, freshly computed chunks and analysis, manifest delta (or rename rekey), doc generation targets, pre-computed docs (from comments or reuse), model call targets and feature route, and final generated result.

<!-- dth:chunk c6bde106ad1ffb60 -->
## `Pipeline.CodePush`

Orchestrates a code_push job: validates the repository, retrieves changed files, triages them to decide what needs documenting (using LLM for complex cases if not a forced push), updates the knowledge graph, generates docs for targets (with spend guard blocking), assembles generated docs into files, lands them as a PR, and persists chunks, vectors, and doc metadata. Returns an aborted outcome if the repo is disabled/untracked or if triage marks the push as cosmetic, and a done/blocked outcome otherwise. Dry-run skips paid calls and writes, returning an estimated token count.

<!-- dth:chunk 75937f6447b6c431 -->
## `Pipeline.chunk`

Computes fresh chunks for a file by analyzing its new content, calculates the manifest delta (added/changed/removed/revived chunks) by comparing to stored chunks, and builds doc generation targets from delta or full-file depending on the change type and triage verdict. For renames without structural changes, computes a rekey mapping old chunk IDs to new ones without re-analyzing.

<!-- dth:chunk e2aad50196a45691 -->
## `Pipeline.generate`

Routes targets to doc sources (unchanged docs, reuse, comments, fast route, full docgen) before any paid calls, builds scoped context per file (with imported symbol definitions from cross-file dependencies), and generates docs in parallel via the model. Caches generated docs for reuse in retries. Records token savings for reused and no-call docs. Skips generation entirely if no docgen route is configured or if dry-run is set.

<!-- dth:chunk 92c7da336e626650 -->
## `fileWork.merge`

Merges generated docs from the model with pre-computed docs (from comment or reuse), ordered by the target chunks. Returns a Result with all sections placed at their chunk IDs, preferring pre-computed docs when available.

<!-- dth:chunk 73a7a4b79f57d4f5 -->
## `Pipeline.persist`

Persists chunks (code and generated docs), vectors, and Palace metadata after docs land: applies manifest deltas to source chunks, renames and re-keys chunks for moved code, processes generated doc chunks (which are searchable), handles chunk deletion, embeds vectors (with graceful degradation on spend block or model mismatch), and stores doc files and their section metadata in the Docs port. Saves token costs from renaming in the Savings port.

<!-- dth:chunk efe015772d557586 -->
## `Pipeline.updateGraph`

Updates the knowledge graph with graph data extracted from changed files, enabling the Architecture view and Palace to reflect the push immediately. Handles file renames by replacing the old path's graph with empty data, replaces the new path with fresh graph data, and (if a service name is configured) adds a deployed-as edge from the repo to the service. Runs before any doc generation, so architecture updates even when docs fail due to spend limits or model errors.

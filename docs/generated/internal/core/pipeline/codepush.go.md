<!-- dth:generated source="internal/core/pipeline/codepush.go" — edit only inside dth:human blocks -->
# `internal/core/pipeline/codepush.go`

Processes code_push jobs by indexing repository changes, triaging files for documentation, and either generating docs PRs (v1) or enqueuing Hub-based documentation generation (v2).

<!-- dth:chunk c6bde106ad1ffb60 -->
## `Pipeline.CodePush`

Handles a code_push job by validating the payload and repository state, comparing git references to identify changed files, triaging them for documentation need (with LLM analysis if required), chunking relevant files, generating documentation (or enqueuing for Docs v2 generation), and landing docs as a PR (for v1) or persisting to the index (both versions). Returns JobAborted for disabled repos, missing repos, or cosmetic changes; early exits are optimized to record token savings. Supports dry-run mode, full re-indexing, forced-path processing, and respects .dthignore filters. Tracks landing status and notes in the result.

<!-- dth:chunk a0bd7b807361b0b3 -->
## `Pipeline.codePushV2`

Indexes pushed code and enqueues repository documentation generation for Docs v2 (Hub-based). On dry runs, immediately queues a `RepoDocs` job. Otherwise, persists the indexed code and last-processed SHA, then enqueues a `JobRepoDocs` task with deduplication keyed to the commit SHA. The enqueue reason defaults to "push {short_sha}" if not provided. Logs indexed file and chunk counts.

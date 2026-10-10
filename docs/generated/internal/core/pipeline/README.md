<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/core/pipeline`

- [`codepush.go`](codepush.go.md) — Processes code_push jobs by indexing repository changes, triaging files for documentation, and either generating docs PRs (v1) or enqueuing Hub-based documen...
- [`deps.go`](deps.go.md) — Defines the Pipeline dependency container that supplies all services to documentation job handlers.
- [`repodocs.go`](repodocs.go.md) — Implements the repository documentation generation pipeline job handler.

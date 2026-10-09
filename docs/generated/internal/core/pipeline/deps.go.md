<!-- dth:generated source="internal/core/pipeline/deps.go" — edit only inside dth:human blocks -->
# `internal/core/pipeline/deps.go`

Defines the Pipeline dependency container that supplies all services to documentation job handlers.

<!-- dth:chunk 9ef93207e6e2ac8e -->
## `Pipeline`

Dependency container holding all services needed by job handlers in the documentation pipeline. Includes data stores (repos, chunks, graphs, docs), LLM infrastructure (gateway, doc generator, indexer), and callbacks for notifying external systems of progress and architectural changes. Configuration fields control documentation generation behavior: `DocGenParallel` sets concurrent file processing, `DocCache` and `DocMode` optimize cost, `DocsV2` enables repository-level document generation via `RepoDocsGen`, and `DocsBudget` enforces spending limits. The `OnChanges` callback receives changed files from pushes (for architecture sync), `Progress` reports ongoing job status, and `Enqueue` queues follow-up jobs like repository documentation tasks.

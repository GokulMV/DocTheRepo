<!-- dth:generated source="cmd/hub/wire.go" — edit only inside dth:human blocks -->
# `cmd/hub/wire.go`

<!-- dth:chunk b0d4863acc8fd57f -->
## `app`

The composition root struct containing all application adapters and core services—configuration, logging, storage, queue, authentication, code hosts, vector index, document/chunk stores, pipeline, lifecycle management, ingestion services (signals, knowledge), aggregation, RAG engine, LLM pool, security scanning, and architecture tracking—initialized once by `wire` and shared across all roles.

<!-- dth:chunk d55b668ee8f9a791 -->
## `wire`

Initializes the application's dependency graph: instantiates stores (connectors, repos, browse, PRs), configures spend guard, LLM pool, vector index (Qdrant or pgvector), and grammar registry; wires up signal ingestion with pollers (CloudWatch, GCP Logging, Wiz, Splunk, and message buses), document pipelines with indexing and decoding, authentication (OIDC if configured), RAG engine with optional sifting, suggestions, security scanning, and knowledge synchronization (Confluence, Jira, Notion). Returns error if configuration or initialization fails.

<!-- dth:chunk 50780c95cf04c34a -->
## `app.registerHandlers`

Registers job type handlers in the queue pool: code push and doc import via pipeline, reindexing, signal batch polling, issue decoding, knowledge sync, security scans and fixes, and PR reviews. The PR review handler unmarshals the payload and delegates lifecycle state management to the sweeper.

<!-- dth:chunk b7aedfd56b39b7ed -->
## `app.tasks`

Defines Phase 4 leader-elected scheduler maintenance tasks: library shelf seeding (24h), signal maintenance (6h), git polling (30s), known-issue auto-suggestion (24h), signal connector polling (15s), knowledge connector syncing (30s), PR lifecycle sweep, session/cache/doc cache garbage collection, spend guard reload (1m), and soft-deleted chunk cleanup with vector index deletion.

<!-- dth:chunk 5d67f8a87c6ba576 -->
## `app.v1Routes`

Mounts authenticated REST API routes for ask/RAG, browse, knowledge upload, security scanning/planning, administration (with configuration, provider testing, docs generation), operations, issue management with known-issue rules, sealed storage, architecture tracking, and GitHub OAuth flow. Each route closure receives admin dependencies pre-configured with stores, handlers, and callbacks for state invalidation and generation.

<!-- dth:chunk d2bda0d65ef66ec6 -->
## `app.securityPlan`

Estimates the security vulnerabilities a scan would find for given modules without invoking a model. Retrieves the repository and its host connector, fetches the branch head commit, and delegates to the scanner's plan operation to provide a preliminary assessment of what would be scanned.

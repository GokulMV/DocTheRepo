<!-- dth:generated source="cmd/hub/wire.go" — edit only inside dth:human blocks -->
# `cmd/hub/wire.go`

Configures and initializes all application dependencies and wires them together into a cohesive system for document generation, retrieval, and signal processing.

<!-- dth:chunk b0d4863acc8fd57f -->
## `app`

The composition root holding every adapter and core service built once and shared across request handlers and background jobs. Includes configuration, storage layers, LLM and vector backends, code host connectors, RAG/Ask engines, signal/issue aggregation, knowledge sync, security scanning, MCP servers, and cached repository linking state.

<!-- dth:chunk d55b668ee8f9a791 -->
## `wire`

Dependency injection function that wires together all application components from configuration, storage, queue, secrets, and logging. It initializes and connects approximately 30 major subsystems including connectors, LLM gateway with spend enforcement, vector index (Qdrant or pgvector), signal ingestion and polling from multiple sources (Kafka, AWS services, Pub/Sub, RabbitMQ, CloudWatch, GCP Logging, Splunk, Wiz), document processing pipeline, RAG engine, security scanning, knowledge base synchronization, architecture tracking, authentication (with optional OIDC), and observability metrics collection. Returns an error if critical initialization fails (spend limits, authentication setup, grammar loading), but gracefully degrades on non-critical failures like unavailable SSO settings. Also configures conditional features such as doc generation v2 and AI-powered result filtering (sift) based on configuration.

<!-- dth:chunk 50780c95cf04c34a -->
## `app.registerHandlers`

Registers job type handlers with the queue pool, mapping job types (CodePush, RepoDocs, signals, issue decode, etc.) to their corresponding pipeline, ingest, or security service handlers.

<!-- dth:chunk 5d67f8a87c6ba576 -->
## `app.v1Routes`

Returns a slice of route mount functions for authenticated APIs, including Ask/RAG queries, repository browsing, document uploads, security scanning, admin settings, MCP servers, and repo-specific docs management (generation, budgeting, export). Each route function accepts a Chi router and registers its endpoints.

<!-- dth:chunk e697d5b35e457362 -->
## `mcpToolBox`

mcpToolBox gives Ask's agent the MCP connections' tools.

<!-- dth:chunk 061b7bd2e3fd5803 -->
## `mcpToolBox.AgentTools`

Converts MCP tools from the manager to a standardized agent tool format for the Ask RAG engine, extracting name, server, description, and schema fields.

<!-- dth:chunk f075f6ef84516dda -->
## `mcpToolBox.CallTool`

Calls an MCP tool by role and name with arguments, returning only the text result and error while discarding other response data from the underlying manager.

<!-- dth:chunk 75554a4ca6eaad80 -->
## `docsCapKey`

docsCapKey is the app setting holding a repository's monthly docs cap in US dollars.

<!-- dth:chunk 413623e069906cae -->
## `app.docsBudget`

Retrieves or computes a repository's monthly documentation budget cap and current spend. Returns the per-repository cap first (from UI settings), then the config default if present. If neither exists and an estimate function is provided, computes a cap as twice the full documentation write estimate (minimum $10) and persists it to settings.

<!-- dth:chunk b69767d210422f57 -->
## `app.estimateDocs`

Estimates the cost of generating or updating all repository documentation by running a dry-run RepoDocs pipeline job and extracting the result. Returns early with zero result if the job fails, prioritizing partial data over blocked documentation.

<!-- dth:chunk 0c4e6a7c4cd63e1b -->
## `app.exportDocs`

Creates a pull request exporting repository documentation as Markdown files under the configured docs path. Fetches documents and repository config, writes files with relativized citation links, and opens a PR with details about the export source commit and document count. Fails if no documents exist.

<!-- dth:chunk 10958149d78d1c0a -->
## `sortedKeys`

Returns sorted keys from a string map in lexicographic order.

<!-- dth:chunk 42ddcf49a26f9983 -->
## `app.hasSystem`

Caches whether tracked repositories reference each other (for the System page) for one minute, querying the RepoDocs store only after the cache expires. Holds the cache state and timestamp under a mutex for thread safety.

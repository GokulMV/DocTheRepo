<!-- dth:generated source="internal/store/gen/models.go" — edit only inside dth:human blocks -->
# `internal/store/gen/models.go`

<!-- dth:chunk 3e2164aa66c14770 -->
## `AnswerCache`

Caches QA answers with question embeddings, citations, and access statistics; includes scope filtering and model information for retrieval and reuse tracking.

<!-- dth:chunk e8c87b7a320141fd -->
## `AppSetting`

A key-value configuration store for application-level settings with automatic update tracking.

<!-- dth:chunk 17afeaada8246600 -->
## `ArchitectureDiagram`

Stores generated architecture diagrams for a repository with metadata including generator type, HTML content, and file size for versioning and tracking changes by commit.

<!-- dth:chunk b868adcd79136ff6 -->
## `AuthSetting`

Manages OpenID Connect authentication configuration with encrypted secrets, including optional password authentication and OIDC-specific settings like allowed domains and groups claim.

<!-- dth:chunk c1107e3f362e837d -->
## `Connector`

Represents a data connector integration with type, encrypted credentials, webhook configuration, poll settings, and health monitoring including last sync and error state.

<!-- dth:chunk 659707c3ec25a4aa -->
## `CostTable`

Pricing table for LLM models with per-token costs for input, output, and embedding, plus optional cache operation costs; includes verification and source tracking.

<!-- dth:chunk 2db30751f444194a -->
## `DocCache`

Caches generated documentation snippets indexed by content hash and symbol, tracking model used and access patterns via creation and usage timestamps.

<!-- dth:chunk c76e58f2bf2557b3 -->
## `Job`

Represents an asynchronous background job with execution state, payload, retry logic, locking for concurrent processing, correlation tracking, and progress/result storage.

<!-- dth:chunk 0fad7b9b326cdffc -->
## `KnowledgeDoc`

Represents a document imported from external connectors with source metadata, content hash for deduplication, processing status, and optional upstream update tracking.

<!-- dth:chunk 34a0f84730000012 -->
## `LlmProvider`

Represents an LLM provider configuration with type, encrypted key, optional extra settings, and PII redaction control; tracks enablement and key management timestamps.

<!-- dth:chunk 987d46c176192378 -->
## `QaMessage`

Represents a message in a QA thread with role, content, citations, model/provider info, token usage, caching status, optional user feedback, and investigation flag.

<!-- dth:chunk c447d0acbd3ab064 -->
## `SealKey`

Represents a cryptographic key pair for post-quantum encryption using X25519 and ML-KEM with encrypted private key storage and optional retirement tracking.

<!-- dth:chunk 1f0bcfdbddaca731 -->
## `SecurityFinding`

Represents a security vulnerability finding with location details, severity metrics, reproduction/fix steps in JSON format, and status tracking including PR links and fix errors.

<!-- dth:chunk c7e24cb323b3b73e -->
## `SecurityModuleCache`

Caches security analysis results for a module keyed by inputs hash, storing findings as JSON and creation timestamp for reuse across scans.

<!-- dth:chunk 3c74e24331e776b0 -->
## `SecurityScan`

Records a security scan execution with status, modules scanned, verdict, and JSON summary; tracks job association, initiator, and optional completion timestamp.

<!-- dth:chunk df6571b0e67c3864 -->
## `UsageEvent`

Records LLM feature usage with model, tokens, cost, latency, and caching; optionally links to repo/user/job/issue and distinguishes between cached and estimated costs, including cache read/write tokens.

<!-- dth:chunk 22d66f4b99ae19c4 -->
## `UserInvite`

Represents an invitation for user signup with hashed token for lookup, optional creator reference, expiration time, and optional usage tracking for single-use invites.

<!-- dth:chunk 26deb329610e3249 -->
## `__module__`

Defines enum constants for domain model states across connector modes, LLM features/providers, job statuses, QA roles, user roles, health states, and various configuration options used throughout the data model.

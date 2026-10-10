<!-- dth:generated source="internal/config/config.go" — edit only inside dth:human blocks -->
# `internal/config/config.go`

This file defines configuration structures and functions for loading, defaulting, and validating application settings from YAML and environment variables.

<!-- dth:chunk 7a02ddbd6b28ad53 -->
## `DocsConfig`

Holds configuration for automatic documentation generation for newly connected repositories. `DefaultPath` specifies where generated docs are stored; `PRSweepInterval` controls how often the scheduler processes documentation PRs (merging green ones, rebasing conflicted ones, closing stale ones); `GenerationMode` selects the generation strategy (thorough, balanced, or economy); `Version` picks the documentation format (version 2 generates comprehensive readable documents per repository kept in the Hub; version 1 generates one doc per source file as a PR); `RepoMonthlyCapUSD` caps the monthly cost per repository, pausing docs generation when reached (0 means the cap is set from the first estimate).

<!-- dth:chunk b395d30fd0d8349a -->
## `SpendConfig`

Holds configuration flags governing spend limit enforcement. `AllowUnlimited` and `AllowUnreportedUsage` are acknowledgements that override spending restrictions. `BufferPct` specifies a safety margin (in percent, default 5%, minimum $0.01) kept free under every spending cap; calls stop at cap minus buffer. Set to 0 to disable buffering. The environment variable `DTH_SPEND_BUFFER_PCT` controls this value.

<!-- dth:chunk 1cdb7d58cd6af760 -->
## `Default`

Returns a fully initialized configuration with sensible defaults for all fields. Sets up server listening on `<IP>:8080`, database with 20 connections, local file-based secrets, email via SMTP, OIDC authentication with standard OAuth scopes, job queue with predefined concurrency limits per job type, JSON logging at info level, vector database support, and document generation settings. Applies to standard home directory locations where applicable.

<!-- dth:chunk 2f9453bd7d5dfaa7 -->
## `applyEnv`

Applies DTH_* environment variables to configuration fields, overriding YAML values. Handles string fields (listen address, auth endpoints, etc.), list fields (roles, allowed domains), booleans (require sealed secrets, tracing), integers (Ask agent steps), floats (similarity thresholds, spend buffer), and durations (PR sweep interval). Validates numeric and duration ranges on parse, returning formatted errors for invalid values like out-of-range agent steps or malformed floats.

<!-- dth:chunk f4dcf597786fe80e -->
## `Config.normalize`

Normalizes configuration after loading by ensuring the docs default path ends with a forward slash if non-empty, and backfills newly-added job types into the concurrency map with sensible defaults (security_scan and security_fix get 1 worker, repo_docs and system_docs get 2 and 1 respectively) only if the queue concurrency map exists but doesn't already specify them.

<!-- dth:chunk 22d91ce927492c64 -->
## `Config.Validate`

Validates all configuration fields and returns accumulated errors in a single message. Checks roles are recognized, required fields are set (listen, database.url_env, auth endpoints for OIDC), numeric ranges (buffer percentage 0–50, similarity 0.8–1, session timeouts, queue concurrency, attempt limits), enum values (auth mode, secrets provider, logging format/level, ask.sift, vector backend), dependencies (email.from required if SMTP is set, QdrantURL required for Qdrant backend), and derived constraints (max_conns ≥ 2, session_idle ≤ session_absolute, backoff_max ≥ backoff_base).

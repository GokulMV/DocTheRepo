<!-- dth:generated source="internal/config/config.go" — edit only inside dth:human blocks -->
# `internal/config/config.go`

Defines configuration structures and loading logic for the DocTheRepo application, with defaults, environment variable overrides, and validation.

<!-- dth:chunk 7a02ddbd6b28ad53 -->
## `DocsConfig`

Holds configuration for automatic documentation generation for newly connected repositories. `DefaultPath` specifies where generated docs are stored; `PRSweepInterval` controls how often the scheduler processes documentation PRs (merging green ones, rebasing conflicted ones, closing stale ones); `GenerationMode` selects the generation strategy (thorough, balanced, or economy); `Version` picks the documentation format (version 2 generates comprehensive readable documents per repository kept in the Hub; version 1 generates one doc per source file as a PR); `RepoMonthlyCapUSD` caps the monthly cost per repository, pausing docs generation when reached (0 means the cap is set from the first estimate).

<!-- dth:chunk 1cdb7d58cd6af760 -->
## `Default`

Returns a fully-initialized configuration where every field has a working default value. It sets up server listening on `<IP>:8080`, database with 20 max connections, local file-based secrets, local authentication, queue workers for various job types, JSON logging at info level, documentation generation in version 2 format with 5-minute PR sweep intervals, pgvector backend, and sensible defaults for retention, grammar loading, and AI ask features. Used as the base configuration before applying overrides from files or environment variables.

<!-- dth:chunk 2f9453bd7d5dfaa7 -->
## `applyEnv`

Maps DTH_* environment variables onto configuration fields that operators commonly customize per deployment environment. Handles string values for server addresses, authentication settings, and paths; parses DTH_ROLES as a comma-separated list; converts boolean strings for sealed secrets requirement and user read access; parses integer durations for PR sweep interval and agent steps (validating 0-10 range); parses floats for similarity thresholds and sift probabilities; and validates numeric environment variables before assigning them, returning formatted errors for invalid values.

<!-- dth:chunk f4dcf597786fe80e -->
## `Config.normalize`

Normalizes configuration after loading by ensuring the docs default path ends with a forward slash if non-empty, and backfills newly-added job types into the concurrency map with sensible defaults (security_scan and security_fix get 1 worker, repo_docs and system_docs get 2 and 1 respectively) only if the queue concurrency map exists but doesn't already specify them.

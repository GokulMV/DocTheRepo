<!-- dth:generated source="internal/config/config.go" — edit only inside dth:human blocks -->
# `internal/config/config.go`

<!-- dth:chunk d926142a0fc11eaf -->
## `Config`

The top-level configuration structure holding all subsystem settings: server listeners, database, authentication, job queues, logging, vector storage, documentation generation, and application-specific tuning for Ask, email, and settings file handling.

<!-- dth:chunk 31bd083dd66b2696 -->
## `EmailConfig`

Configures email-based delivery of sign-in and password-reset links. When unconfigured, the admin must manually copy and share links. Supports any SMTP provider via environment variables to avoid embedding credentials in config files.

<!-- dth:chunk 348c3ddb8cda0707 -->
## `EmailConfig.SMTPURL`

Reads the SMTP connection URL from the environment variable named in `SMTPURLEnv`, trimming whitespace. Returns empty string if `SMTPURLEnv` is not set, effectively disabling email.

<!-- dth:chunk 008827e6d3e93025 -->
## `AskConfig`

Controls behavior of the Ask feature: agent iterations for multi-step retrieval, answer caching by question similarity, and source filtering via a judge model ("sift"). All settings can be tuned per environment through environment variables and have sensible defaults for cost-aware operation.

<!-- dth:chunk 09f61ae48e149a28 -->
## `SettingsConfig`

Policy for settings files pasted into the UI, controlling which secret reference schemes (env, file, vault, etc.) are available and whether they must be sealed to the Hub's encryption key. Supports inline settings for deployment platforms that cannot mount files.

<!-- dth:chunk 1ab17875f7091b62 -->
## `ServerConfig`

Controls HTTP server binding, metrics collection, public-facing URL for sign-in links, request timeouts, graceful shutdown duration on SIGTERM, and deployment environment labeling (shown in UI to prevent confusion between staging and production).

<!-- dth:chunk 7a02ddbd6b28ad53 -->
## `DocsConfig`

Default path for generated documentation in newly connected repositories and scheduler interval for merging, rebasing, and closing documentation pull requests. Generation mode (thorough/balanced/economy) trades quality for cost.

<!-- dth:chunk 1cdb7d58cd6af760 -->
## `Default`

Returns a fully initialized configuration with working defaults for all subsystems: file-based secret encryption, local authentication, PostgreSQL vector backend, and reasonable queue concurrency limits. Used as the baseline when loading from YAML or environment.

<!-- dth:chunk 2f9453bd7d5dfaa7 -->
## `applyEnv`

Maps DTH_* environment variables onto frequently-tuned configuration keys, overriding YAML values. Includes validation for numeric ranges (agent steps 0-10, similarity 0.8-1, sift probability 0.1-0.95) and duration formats, returning the first error encountered.

<!-- dth:chunk f4dcf597786fe80e -->
## `Config.normalize`

Post-processes configuration: ensures the docs default path ends with `/`, and back-fills newly-added job types (security_scan, security_fix) with default concurrency limits if not already set in the config file.

<!-- dth:chunk 22d91ce927492c64 -->
## `Config.Validate`

Validates all configuration keys against business rules and constraints, collecting all errors and reporting them together for easier debugging. Checks required fields, enum constraints, numeric bounds (session timeouts, pool size, retry attempts), URL format, and job type names. Returns a formatted error listing all problems.

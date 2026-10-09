<!-- dth:generated source="scripts/docs-real-run.sh" — edit only inside dth:human blocks -->
# `scripts/docs-real-run.sh`

Automates end-to-end documentation generation for a repository using a real LLM model, generating reports for output review and prompt tuning.

<!-- dth:chunk a50734912d737044 -->
## `scripts/docs-real-run.sh`

### Overview

This bash script automates the full documentation generation workflow for testing and tuning. It starts a local hub instance against an empty database, configures authentication and API access, sets up a provider and GitHub connector with spending limits, tracks a target repository, waits for documentation generation to complete, and exports the results as markdown and JSON reports.

### Key responsibilities

**Initialization and setup**: Creates temporary working directory, generates random owner password, starts hub service with local auth mode, waits for readiness, and establishes authenticated API session via login and token generation.

**Configuration**: Sets up LLM provider (via `RUN_PROVIDER_KIND` and `RUN_API_KEY`), configures documentation models (`RUN_MODEL` for detailed docs, `RUN_FAST_MODEL` for short code docs), creates GitHub connector with token (`RUN_GITHUB_TOKEN`), and establishes spending cap enforcement with optional pricing data for models the hub doesn't know about.

**Execution and monitoring**: Tracks the target repository (default `GokulMV/DocTheRepo`) with monthly cost cap (default `$3 USD`), polls the documentation job status until completion or timeout (default 40 minutes), logs progress updates, and detects failures in indexing.

**Output**: Generates three report files: `docs-report.md` (full text documentation), `docs-report-summary.md` (summary), `docs-report.json` (structured data), plus `hub-warnings.log` for debugging.

**Required environment**: `DTH_DATABASE_URL` (empty PostgreSQL with pgvector), `RUN_PROVIDER_KIND`, `RUN_MODEL`, `RUN_API_KEY`, `RUN_GITHUB_TOKEN`. Optional: `RUN_PRICE_IN`/`RUN_PRICE_OUT`, `RUN_BASE_URL`, `RUN_GITHUB_API_URL`, `RUN_FAST_MODEL`, `RUN_REPO`, `RUN_CAP_USD`, `RUN_TIMEOUT_MIN`, `RUN_OUT`.

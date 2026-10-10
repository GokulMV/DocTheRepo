<!-- dth:generated source="scripts/docs-real-run.sh" — edit only inside dth:human blocks -->
# `scripts/docs-real-run.sh`

Bash script that orchestrates end-to-end documentation generation by running a hub server, configuring an LLM provider and GitHub connector, and generating reports for a specified repository.

<!-- dth:chunk a50734912d737044 -->
## `scripts/docs-real-run.sh`

This script sets up a complete documentation generation pipeline using a real LLM model. It starts an empty hub instance with local authentication, configures a specified AI provider (Anthropic, OpenAI, etc.) and GitHub connector, tracks a repository with a spending cap, waits for documentation generation to complete, and exports the results to markdown and JSON report files.

Requires environment variables for database URL, provider kind, model name, API key, and GitHub token. Supports optional pricing configuration (for enforcing spend caps), long-prompt tier pricing, alternate base URLs, fast model selection (for code snippets), repository name, spending cap (default $3), timeout (default 40 minutes), and output directory. The script enforces a global monthly spending limit to prevent runaway costs, polls the documentation job status every 10 seconds until completion or timeout, and outputs three report files: full text documentation, JSON format, and markdown summary.

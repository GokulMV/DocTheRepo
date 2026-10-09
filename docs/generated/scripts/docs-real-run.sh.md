<!-- dth:generated source="scripts/docs-real-run.sh" — edit only inside dth:human blocks -->
# `scripts/docs-real-run.sh`

Shell script that orchestrates a complete documentation generation run with a real AI model for testing and tuning DocTheRepo's output.

<!-- dth:chunk a50734912d737044 -->
## `scripts/docs-real-run.sh`

This is a testing and tuning script for DocTheRepo's documentation generation. It orchestrates a complete workflow: starting the hub server against a fresh database, authenticating as a local owner, configuring an AI provider and GitHub connector, tracking a repository with spending limits, and waiting for documentation generation to complete. After the job finishes, it exports the generated documentation and warnings to markdown and JSON files for review.

The script requires environment variables for database access, provider credentials (API key), GitHub access, and optional parameters for model pricing, API endpoints, and resource constraints (spending cap, timeout). It validates that pricing information exists for the selected models to enforce the spending cap, sets up global and per-repository cost limits via the hub API, and polls the job status every 10 seconds until completion or timeout. It captures hub logs and extracts warnings for debugging.

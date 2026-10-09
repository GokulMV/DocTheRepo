<!-- dth:generated source="scripts/quickstart.sh" — edit only inside dth:human blocks -->
# `scripts/quickstart.sh`

Bash script that provides an automated one-command setup and deployment of DocTheRepo from a fresh machine, handling prerequisite installation, optional containerization, and hub initialization.

<!-- dth:chunk d96a39be54bf66c7 -->
## `scripts/quickstart.sh`

Quickstart script for DocTheRepo: orchestrates prerequisite checking, installation, and deployment of the hub and UI from a fresh machine to a running web service.

The script supports multiple modes (native Go/Node toolchain vs. containerized) and handles platform-specific setup for Linux, macOS, and WSL 2. It checks and installs required tools (git, curl, tar, make, openssl, C compiler, Go 1.26.9, Node.js 22.12+, and Docker/Compose). For native mode, builds the release binary and UI locally; for container mode, uses a Docker image. Automatically starts PostgreSQL via Docker Compose, runs the hub server, and opens a one-time password link for owner account setup.

Supports optional environment configuration via settings files, model API keys (Anthropic/OpenAI), custom ports, and email. Rerunning is safe: preserves database and uses stored API tokens. The `--down` option stops the stack; `--down --wipe` also deletes data. The `--reset-password` action generates a new one-time link for password recovery. Uses `set -euo pipefail` for strict error handling and defines utility functions (say, warn, die, have) for consistent output formatting.

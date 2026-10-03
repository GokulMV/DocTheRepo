<!-- dth:generated source="scripts/quickstart.sh" — edit only inside dth:human blocks -->
# `scripts/quickstart.sh`

<!-- dth:chunk d96a39be54bf66c7 -->
## `scripts/quickstart.sh`

This is a comprehensive setup and quickstart script for DocTheRepo that automates the entire process from a fresh machine to a running web UI. It handles prerequisite installation (git, curl, Go 1.25.13, Node.js 22.12+, Docker, C compiler), builds the UI and hub from the current checkout, starts PostgreSQL in Docker, runs the hub service, and opens a browser to a one-time sign-in link.

The script supports three modes: native (uses system or downloaded Go/Node), container (builds hub in Docker), and image (uses a pre-built Docker image). It accepts command-line options for port, email, settings file, and browser control, and environment variables for configuration (DTH_PORT, DTH_OWNER_EMAIL, etc.). Running with `--down` stops the stack; `--down --wipe` also deletes persistent data. The hub creates an API token on first run for subsequent invocations, and stores encrypted secrets with a master.key file in native mode. The script validates database credentials match the current .env file and refuses to start if the master key is missing but an earlier database exists.

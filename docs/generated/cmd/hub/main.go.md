<!-- dth:generated source="cmd/hub/main.go" — edit only inside dth:human blocks -->
# `cmd/hub/main.go`

<!-- dth:chunk f07ca65df6b27987 -->
## `main`

Entry point that dispatches to special-purpose subcommands (`healthcheck`, `invite`, `rotate-key`) or runs the main server. Accepts `-config` and `-roles` flags, with defaults from environment variables or filesystem defaults. Logs fatal errors to stderr and exits with code 1 on failure.

<!-- dth:chunk a5fd877559f9a564 -->
## `run`

Loads configuration, initializes logging, database, and secrets, then orchestrates concurrent startup of API, worker, and scheduler roles based on configuration. Sets up HTTP listeners, a job queue worker pool, signal ingestion, and scheduled maintenance tasks. Handles graceful shutdown on SIGTERM/SIGINT with a configurable grace period for in-flight work. Returns errors from configuration or early startup failures; fatal runtime errors are logged and trigger shutdown.

<!-- dth:chunk 1f58894c3b5c2c77 -->
## `openKEK`

Routes to the configured key-encryption key provider (localfile, awskms, or gcpkms) and returns a KeyEncrypter. The localB64 parameter allows overriding the localfile provider with a base64-encoded key from the DTH_LOCAL_KEY environment variable, useful for containerized deployments. Returns an error if the provider name is unrecognized or if the provider's initialization fails.

<!-- dth:chunk c50e9e2fbd56798f -->
## `openSecrets`

Initializes a secrets box with the resolved key-encryption key by calling openKEK, then probes the key with a seal-unseal cycle to verify KMS access and permissions at startup. Returns an error if the key is unusable, catching missing KMS permissions early rather than on first use.

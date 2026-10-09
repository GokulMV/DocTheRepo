<!-- dth:generated source="cmd/hub/main.go" — edit only inside dth:human blocks -->
# `cmd/hub/main.go`

Entry point for the hub service that orchestrates configuration, database, logging, background workers, and HTTP servers.

<!-- dth:chunk a5fd877559f9a564 -->
## `run`

Initializes and runs the hub server with all configured components. It loads configuration, sets up logging and metrics, opens the database (optionally running migrations), loads encryption keys, then spins up configured roles: Worker (job processing, stream consumers, reload guard), API (HTTP server with optional email), Scheduler (recurring tasks like lease reclamation and partition management), and metrics server. Components run concurrently with graceful shutdown on interrupt/SIGTERM, draining with a configurable grace period; expired job leases are reclaimed during shutdown.

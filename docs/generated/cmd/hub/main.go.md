<!-- dth:generated source="cmd/hub/main.go" — edit only inside dth:human blocks -->
# `cmd/hub/main.go`

Entry point for the hub service that orchestrates configuration loading, service initialization, and role-based component startup (API, worker, scheduler).

<!-- dth:chunk a5fd877559f9a564 -->
## `run`

Initializes and runs the hub server with the provided config path. Loads configuration and database, sets up logging and observability, opens the secrets box, and wires together API, worker, and scheduler components based on enabled roles. Spawns goroutines to run the worker pool (with job handlers and stream consumers), scheduler tasks, aggregator, signal reloader, and HTTP servers for API and metrics. Handles graceful shutdown on interrupt or SIGTERM, with a configurable grace period for in-flight work before forceful exit. Returns any fatal startup error or server error that triggers shutdown.

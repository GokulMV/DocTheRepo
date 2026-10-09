<!-- dth:generated index — edit only inside dth:human blocks -->
# `cmd/hub`

- [`main.go`](main.go.md) — Entry point for the hub service that orchestrates configuration, database, logging, background workers, and HTTP servers.
- [`rotate.go`](rotate.go.md) — Implements database key rotation functionality for re-encrypting sealed secrets across multiple tables in the hub service.
- [`wire.go`](wire.go.md) — wire.go bootstraps and wires all application dependencies and services for the hub service.

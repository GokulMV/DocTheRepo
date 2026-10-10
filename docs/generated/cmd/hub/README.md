<!-- dth:generated index — edit only inside dth:human blocks -->
# `cmd/hub`

- [`main.go`](main.go.md) — Entry point for the hub service that orchestrates configuration, database, logging, background workers, and HTTP servers.
- [`rotate.go`](rotate.go.md) — Implements database key rotation functionality for re-encrypting sealed secrets across multiple tables in the hub service.
- [`wire.go`](wire.go.md) — Configures and initializes all application dependencies and wires them together into a cohesive system for document generation, retrieval, and signal process...

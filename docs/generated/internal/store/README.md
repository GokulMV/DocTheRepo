<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/store`

- [`gen`](gen/README.md)
- [`queries`](queries/README.md)
- [`analytics.go`](analytics.go.md) — Package store provides database access for analytics and browsing functionality in DocTheRepo.
- [`architecture.go`](architecture.go.md) — Manages retrieval and storage of repository architecture documentation, including entities, relationships, diagrams, and external documentation links.
- [`chunks.go`](chunks.go.md) — chunks.go provides database access and manipulation for code chunks, including upsert, delete, retrieval, and conversion operations.
- [`decodes.go`](decodes.go.md) — Decodes.go implements a decode store adapter backed by a database connection, handling persistence and retrieval of decoded documentation chunks.
- [`mcp.go`](mcp.go.md) — Provides persistent storage and management of MCP (Model Context Protocol) server connections with encrypted secrets and OAuth tokens.
- [`qa.go`](qa.go.md) — Implements Q&A thread and message storage operations for persisting conversational interactions with source citations and AI model metadata.
- [`repodocs.go`](repodocs.go.md) — Provides database storage and retrieval for repository documentation (Docs v2), file cards, and repository facts extracted from the knowledge graph.
- [`system.go`](system.go.md) — Provides database storage operations for repository documentation, including loading wiring facts and calculating inter-repository system links.

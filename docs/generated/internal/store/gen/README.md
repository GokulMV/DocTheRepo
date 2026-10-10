<!-- dth:generated index — edit only inside dth:human blocks -->
# `internal/store/gen`

- [`analytics.sql.go`](analytics.sql.go.md) — Generated SQL query methods for analytics, providing query execution and result scanning for source picker activity, pipeline statistics, connector health, u...
- [`chunks.sql.go`](chunks.sql.go.md) — Generated database query methods and SQL constants for managing documentation chunks with support for soft-deletion, versioning, and multi-repository indexing.
- [`llm.sql.go`](llm.sql.go.md) — Generated Go code wrapping SQL queries for managing LLM provider configurations, routing rules, cost tables, and usage tracking in the database.
- [`models.go`](models.go.md) — Defines generated model structures for the cost storage layer, including pricing data structures that serialize to JSON for database persistence.
- [`qa.sql.go`](qa.sql.go.md) — Generated Go database access code for QA-related operations, translating SQL queries to typed methods and parameter structs.
- [`signals.sql.go`](signals.sql.go.md) — Generated database query methods for managing signals, issues, known issues, and code chunks.

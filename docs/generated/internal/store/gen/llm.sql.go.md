<!-- dth:generated source="internal/store/gen/llm.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/llm.sql.go`

Generated Go code wrapping SQL queries for managing LLM provider configurations, routing rules, cost tables, and usage tracking in the database.

<!-- dth:chunk a4f156d6d0347912 -->
## `Queries.ListCostTable`

Executes the `listCostTable` SQL query to retrieve all cost table entries from the database, ordered by provider kind and model. It scans each row into a `CostTable` struct containing pricing information for various LLM operations (input, output, embedding, cache operations, and long-context variants), then returns the complete list or an error if the query fails.

<!-- dth:chunk 1e0599cb7bc6e80f -->
## `__module__`

SQL query constant definitions for the LLM provider store layer, generated from SQL files. Defines parametrized queries for managing LLM providers (insert, update, delete, list), model routes (get, list, upsert), usage events (insert), spend limits (list), and cost table lookups (list, calculate spending since a timestamp). Each constant contains a commented directive specifying the query name and return type (`:exec`, `:execrows`, `:one`, or `:many`).

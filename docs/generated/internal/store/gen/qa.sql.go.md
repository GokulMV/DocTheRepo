<!-- dth:generated source="internal/store/gen/qa.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/qa.sql.go`

Generated Go database access code for QA-related operations, translating SQL queries to typed methods and parameter structs.

<!-- dth:chunk 5bfbeaa509d819a3 -->
## `Queries.ChunksByIDsScoped`

Retrieves chunks by their IDs with repository scoping applied. Executes the `chunksByIDsScoped` SQL query, filtering by chunk IDs, repo access (all repos, specific repo IDs, or accessible external sources), and repo requirements. Returns a slice of `Chunk` structs or an error if the query fails.

<!-- dth:chunk c1817223880c0372 -->
## `Queries.ChunksForPath`

Retrieves chunks matching a file path for the agent's "read file" operation. Matches chunks where the path equals or ends with the given path, filters by repo access and optional source types, and sorts by scope and path. Returns matching `Chunk` structs or an error; results are limited by the `Lim` parameter.

<!-- dth:chunk b3ffdc93f28df198 -->
## `InsertMessageParams`

Parameters for inserting a QA message into the database. Captures message identity, content, role, LLM metadata (provider, model, token counts, cost), and analysis fields (citations, sift scores, confidence, caching and investigation flags).

<!-- dth:chunk 7032eb0a2555a7bd -->
## `Queries.InsertMessage`

Inserts a QA message with the provided parameters into the database. Executes the `insertMessage` SQL query and returns any error encountered; used to persist both user questions and assistant responses in QA threads.

<!-- dth:chunk 7daec2d1271b271a -->
## `Queries.OverviewChunks`

Retrieves overview material for broad repository questions, ranked by relevance. Returns Hub Overview and Architecture documents first, then READMEs and architecture/overview docs, then generated entry-point docs. Results are filtered by repo access and source types, returning `OverviewChunksRow` structs with chunk ID and rank.

<!-- dth:chunk 80d55e05247b0c14 -->
## `Queries.ThreadMessages`

Fetches all messages for a QA thread ordered by creation time (with questions before answers). Returns a slice of `QaMessage` structs containing questions, answers, feedback, and LLM metadata; used to reconstruct complete conversation history for a thread.

<!-- dth:chunk 79f17f66b91400b6 -->
## `__module__`

SQL query string constants for QA operations including chunk retrieval, thread management, cached answers, full-text search, and message persistence. These constants define parameterized PostgreSQL queries that are executed by corresponding `Queries` methods.

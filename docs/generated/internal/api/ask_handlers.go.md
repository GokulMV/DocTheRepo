<!-- dth:generated source="internal/api/ask_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/ask_handlers.go`

<!-- dth:chunk 63ada4922b70b966 -->
## `askResponse`

Response structure for ask API queries. Includes the conversation thread and message IDs, the model's answer text, citations from source code with line numbers, token usage metrics, and two optional fields: `Investigated` indicates the model expanded its search when initial retrieval was insufficient, and `Sift` contains a summary of how the RAG system selected sources before generating the answer.

<!-- dth:chunk 84e0089298c69f1c -->
## `askHandlers.ask`

HTTP handler for ask endpoint. Validates the user's rate limit, normalizes and validates the question (1–4000 characters), restricts the query to accessible repositories, retrieves chat history from an optional thread ID (up to `HistoryTurns` messages), and invokes the RAG engine. Supports streaming responses via Server-Sent Events (SSE) when the client sends `Accept: text/event-stream`; otherwise returns JSON. Creates or retrieves the thread, stores the question-answer exchange, and returns citations, token usage, and model metadata. Errors at any step are handled via `askErr` to ensure proper cleanup of streaming state.

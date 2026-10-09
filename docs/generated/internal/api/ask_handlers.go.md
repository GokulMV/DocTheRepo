<!-- dth:generated source="internal/api/ask_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/ask_handlers.go`

Defines HTTP handlers for answering questions using a RAG-backed AI engine with conversation history, streaming support, rate limiting, and repository access control.

<!-- dth:chunk 63ada4922b70b966 -->
## `askResponse`

HTTP response body for an ask query, containing the AI-generated answer, metadata about the query, and debugging information. The `Investigated` field indicates the model performed expanded retrieval when initial results were insufficient. The `Sift` field describes how sources were selected (omitted if source picking did not run), and `Confidence` provides trust metrics and reasoning for the answer. `Usage` breaks down token counts and API costs for the exchange.

<!-- dth:chunk 84e0089298c69f1c -->
## `askHandlers.ask`

Handles POST requests to answer questions with optional streaming and conversation history. Enforces per-user rate limiting, validates and normalizes the question, restricts repository access based on the caller's permissions, and preserves up to `HistoryTurns` prior messages from a conversation thread if provided. Delegates the answer generation to the RAG engine; if the request accepts `text/event-stream`, streams deltas, status updates, citations, and the final response via SSE, otherwise sends a single JSON response. Creates a new thread on first question or adds the exchange to an existing one, returning thread ID, message ID, answer text, citations, and token usage.

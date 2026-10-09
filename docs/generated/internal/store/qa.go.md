<!-- dth:generated source="internal/store/qa.go" — edit only inside dth:human blocks -->
# `internal/store/qa.go`

Implements Q&A thread and message storage operations for persisting conversational interactions with source citations and AI model metadata.

<!-- dth:chunk da69791737c03d98 -->
## `Message`

Represents a single message (one turn) in a Q&A thread. Key fields include `Role` (user or assistant), `Content`, and `Citations` (source references as JSON). For assistant messages, `Usage` tracks token counts and cost; `Cached` indicates response reuse. `Investigated` marks when the initial search was insufficient and a wider search was performed. `Sift` and `Confidence` store JSON-serialized metadata about source trimming and answer trustworthiness respectively. `Feedback` captures optional user feedback on the answer.

<!-- dth:chunk 2a477bfae4dbaec0 -->
## `QA.GetThread`

Retrieves a thread by ID with ownership validation—returns `ErrNotFound` if the thread doesn't exist or doesn't belong to the specified user. Validates the ID format with a UUID regex before querying. If `withMessages` is true, fetches and maps all thread messages, parsing assistant-specific fields (usage tokens, cost) and handling optional feedback. Returns both the thread metadata and its messages if requested.

<!-- dth:chunk 23b758de12ec6519 -->
## `QA.AddExchange`

Stores a question-answer exchange as two messages in a thread within a transaction: inserts a user message with the question, then an assistant message with the answer containing citations, model info, token usage, and computed metadata (sift summary and confidence). Serializes complex fields like citations and confidence to JSON. Returns the generated ID of the assistant message. Updates the thread's modification timestamp before committing.

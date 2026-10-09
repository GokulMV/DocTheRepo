<!-- dth:generated source="internal/core/rag/rag.go" — edit only inside dth:human blocks -->
# `internal/core/rag/rag.go`

Implements a retrieval-augmented generation engine that answers questions by combining cached answers, semantic search, source sifting, and agentic investigation with cost and performance tracking.

<!-- dth:chunk b036deb95a402827 -->
## `Engine`

Coordinates question-answering by orchestrating retrieval, caching, sifting, and agentic investigation. Holds a document store, vector index, LLM gateway, optional cost tracking, performance observation, and tools for the agent to call. The `AgentSteps` field enables multi-step investigation when retrieval is insufficient; `SimilarAnswer` enables semantic answer reuse via embedding similarity; `Sift` provides cheap source validation and index exploration.

<!-- dth:chunk e096a57653130987 -->
## `Query`

A question to be answered, with optional chat history for follow-ups, user context (ID and role), and callbacks for streaming (text deltas, status updates, answer resets). History supports multi-turn conversation; Role restricts which connected tools the agent may access; OnDelta and OnStatus enable live feedback during retrieval and agent steps.

<!-- dth:chunk b3982818da1ba9eb -->
## `Answer`

The structured answer returned by `Engine.Ask`, containing the text, source citations, token usage, cost, and metadata. `Cached` indicates a direct cache hit; `Investigated` marks answers requiring agent exploration; `Sift` and `Confidence` provide optional source-picking and trust summaries. Model and Provider identify the LLM used.

<!-- dth:chunk 9b579c45f02d1b84 -->
## `Engine.Ask`

Answers a query by first checking exact and semantic caches (skipped for follow-ups with history), retrieving chunks, optionally sifting sources for relevance, and invoking the agent if retrieval is too sparse or the question targets live data. Streams text via OnDelta and status via OnStatus, resets via OnReset if investigation finds citations for an initially uncited answer, and caches new answers with citations. Returns error if the question is empty or over 4000 characters, or if routing or LLM calls fail. Aggregates token usage and cost from retrieval, sifting, and agent steps.

<!-- dth:chunk a58bd4d5f86bf51d -->
## `sourceType`

Maps a chunk's source type to a user-facing label: code, confluence (team pages), issue, tool (live product results), or doc as default. Used in citation generation to help users understand where answers come from.

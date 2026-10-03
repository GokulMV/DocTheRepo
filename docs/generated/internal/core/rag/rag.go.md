<!-- dth:generated source="internal/core/rag/rag.go" — edit only inside dth:human blocks -->
# `internal/core/rag/rag.go`

<!-- dth:chunk 4730c423cd04a4bc -->
## `Store`

Interface for retrieving chunks and answers; all methods apply the scope in SQL. Supports full-text and symbol-based search, answer caching, and chunk lookup by ID.

<!-- dth:chunk 1b152e1a87691bfa -->
## `Overviewer`

Overviewer is a Store that can find material describing repositories as a whole.

<!-- dth:chunk 8b1f5e5c139d6e95 -->
## `IsOverviewQuestion`

IsOverviewQuestion reports questions about a repository or system as a whole.

<!-- dth:chunk b036deb95a402827 -->
## `Engine`

Orchestrates question answering with retrieval, optional source sifting, agent-based exploration, and caching. Integrates a vector index, LLM gateway, sift judge, and cost tracking.

<!-- dth:chunk e096a57653130987 -->
## `Query`

One question with optional chat history for follow-ups, scope constraints, and callbacks for streamed text, status updates, and answer resets.

<!-- dth:chunk b3982818da1ba9eb -->
## `Answer`

Result of answering: the answer text, citations to sources, token usage, cost, model/provider info, and optional sift metrics and investigation flag.

<!-- dth:chunk 22dc5ddd77ae3f9f -->
## `SiftSummary`

Metrics and costs for one sift operation: candidates evaluated, sources kept, tokens saved from the answer model, judge's token usage and cost, and net savings (negative if the judge cost more than was saved).

<!-- dth:chunk a8f35d76694bd436 -->
## `CacheKey`

Derives a stable hash key from the question's fingerprint and scope (repos and sources), used to cache answers. Fingerprinting allows different rewordings of the same question to share cached answers if the store deems them current.

<!-- dth:chunk 15c95d95572340e0 -->
## `Fingerprint`

Reduces a question to its semantic core by lowercasing, removing filler words ("please", "can you", articles), dropping trailing "s" and "ies", and keeping order but preserving dots and slashes in identifiers. Questions like "Can you explain how payment retries work?" and "how payment retry works" produce the same fingerprint.

<!-- dth:chunk 9b579c45f02d1b84 -->
## `Engine.Ask`

Retrieves and answers a question, using cache for first questions and semantic cache for similar rewording, optionally sifting sources and running the agent to explore when retrieval finds too little. Tracks costs and savings, cites sources, and stops sift operation when sources are judge-confirmed. Follow-up questions skip the cache.

<!-- dth:chunk 32c7c7b531670184 -->
## `Engine.answer`

Calls the answer model with packed chunks via the citation contract, optionally holding back streamed text while it could still be the "not found" sentence. Returns the answer, whether text reached the delta callback, and any error. The model's response is cited using the Cite function.

<!-- dth:chunk fd63a037f8d42a17 -->
## `Engine.retrieve`

Runs hybrid search combining vector search, full-text search, and optional overview retrieval, then fuses results, loads full chunks, and expands via graph neighbors. Every read is ACL-scoped in SQL. Supports reusing a pre-computed embedding and observes stage timings.

<!-- dth:chunk a58bd4d5f86bf51d -->
## `sourceType`

Maps chunk source enum values to human-readable type strings for citation display: code, issue, confluence (for team pages), or generic doc.

<!-- dth:chunk e7616c53d5450160 -->
## `Sources`

Converts API-level source include strings (code, docs, confluence, notion, knowledge, issues) to chunk source enums, or returns an error for unknown values. Empty input means all sources.

<!-- dth:chunk 59e53bb0aa64640c -->
## `packedTokens`

Sums estimated tokens across chunks, adding 30 tokens per chunk as overhead.

<!-- dth:chunk f0163a4f9403d892 -->
## `Engine.siftSummary`

Creates a SiftSummary from a sift report by recording candidate and kept counts, then delegating cost and usage calculation to addSift.

<!-- dth:chunk cb3e6c198fea1d45 -->
## `Engine.addSift`

Accumulates judge usage and costs from a sift or navigation report into a summary, tracking token consumption, calibration status, explored files, model, and USD cost.

<!-- dth:chunk 6707250c1fb5fd3f -->
## `Engine.tree`

Returns a tree for the source picker to explore the index within the question's scope, or nil if the store is not an Explorer.

<!-- dth:chunk cd0594c52e8b5642 -->
## `scopedTree`

Adapter that wraps an Explorer and Scope to provide a sift.Tree interface for bounded index exploration.

<!-- dth:chunk 16cd2cbcf3c53e32 -->
## `scopedTree.Paths`

Lists files reachable within the scope up to the given limit, converting the explorer's Path results to sift.File format.

<!-- dth:chunk 5d6df2ed7bbb07e1 -->
## `scopedTree.Read`

Reads chunks for a file within the scope, fetching up to limit×4 chunks then filtering to keep only those matching the repo and up to limit results.

<!-- dth:chunk 998048852efc2a5d -->
## `__module__`

Module constants defining retrieval, fusion, and expansion parameters (candidate limits, RRF K, budget), plus error messages, regex patterns for overview detection, and a system prompt for the answer model.

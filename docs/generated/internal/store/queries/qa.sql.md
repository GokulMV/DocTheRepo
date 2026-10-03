<!-- dth:generated source="internal/store/queries/qa.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/qa.sql`

<!-- dth:chunk 8030757b28385c94 -->
## `internal/store/queries/qa.sql`

SQL query library for Q&A functionality, including hybrid full-text search on chunks, cached answer retrieval and storage, symbol graph traversal for related code, and thread/message management for multi-turn conversations.

**SearchChunksFTS**: Hybrid keyword search using both English stemming and simple exact-match configs on chunk full-text vectors, filtered by access control (repo/source) and ranked by relevance, returning chunk IDs.

**ChunksByIDsScoped**: Retrieves chunks by their IDs, filtered by access control rules and soft deletion.

**SymbolNeighbors**: Finds one-hop related symbols (callers/callees) in the dependency graph by traversing edges connected to given symbol entity keys.

**GetCachedAnswer**: Retrieves and increments hit counter for a cached answer if it's under 7 days old and all cited chunks still exist with no updates to their scopes.

**PutCachedAnswer**: Inserts or updates a cached answer with its citations and chunk IDs, auto-extracting affected scopes.

**SimilarAnswerCandidates**: Finds recent cached answers in the same scope using the same embedding model for deduplication via embedding similarity.

**SetAnswerMeaning**: Updates a cached answer's question, scope context, embedding model, and vector for similarity search.

**Thread/Message Management**: CreateThread, GetThread, ListThreads, DeleteThread, InsertMessage, ThreadMessages manage multi-turn Q&A conversations; feedback is scope-protected.

**OverviewChunks**: Prioritizes READMEs and architecture docs for broad repository questions, ranked by specificity and path length.

**ChunksForPath** and **ListChunkPaths**: Support file browsing via exact/suffix path matching with chunk aggregation.

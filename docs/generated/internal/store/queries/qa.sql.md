<!-- dth:generated source="internal/store/queries/qa.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/qa.sql`

SQL query definitions for Q&A operations including full-text search, answer caching, thread management, and repository navigation.

<!-- dth:chunk 8030757b28385c94 -->
## `internal/store/queries/qa.sql`

This SQL file contains queries for the QA system, providing full-text search over documentation chunks, caching of answers, management of Q&A threads and messages, and retrieval of repository overview materials and file paths.

**SearchChunksFTS**: Hybrid full-text search combining English stemming and exact identifier matching against a text-search vector, with ACL filtering by repository access and source type, ranked by relevance.

**ChunksByIDsScoped**: Retrieves specific chunks by ID with access control checks for repository membership and source type.

**SymbolNeighbors**: Finds one-hop callers and callees of given symbols by traversing edges in the entity graph, excluding the source symbols themselves.

**GetCachedAnswer**: Retrieves and increments hit counter for cached answers valid within 7 days where all cited chunks still exist and no repository/scope content has changed since caching.

**PutCachedAnswer**: Inserts or updates a cached answer, automatically recording the distinct scopes of its cited chunks and resetting the hit counter.

**SimilarAnswerCandidates**: Finds recent cached answers (within 7 days) from the same scope with the same embedding model and non-null embeddings, ordered newest first for similarity comparison.

**HitCachedAnswer, SetAnswerMeaning, GCAnswerCache**: Increment hit tracking, update question metadata and embeddings, and delete stale cached answers older than 7 days respectively.

**Thread and Message Management**: CreateThread, GetThread, TouchThread, ListThreads, DeleteThread, InsertMessage, ThreadMessages, and SetFeedback handle Q&A thread lifecycle and message storage with user-scoped access control.

**OverviewChunks**: Retrieves repository overview materials (Hub docs, READMEs, architecture files, entry point docs) ranked by type and path length for broad repository questions.

**ChunksForPath, ListChunkPaths**: Support file navigation—retrieving all chunks from a file path and listing distinct paths with chunk counts filtered by text content.

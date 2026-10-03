<!-- dth:generated source="internal/store/gen/qa.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/qa.sql.go`

<!-- dth:chunk cf677a59250788e8 -->
## `ChunksForPathParams`

Parameters for querying chunks by file path, with filters for repository scope, content sources, and result limit. The path can match exactly or as a suffix (e.g., "foo.go" matches "path/to/foo.go").

<!-- dth:chunk c1817223880c0372 -->
## `Queries.ChunksForPath`

Retrieves chunks from files whose path matches or ends with the given path, optionally filtered by repositories and sources. Returns all matching chunks ordered by scope, path, and chunk ID.

<!-- dth:chunk da6b22dfc2fd091e -->
## `Queries.GetCachedAnswer`

Retrieves a cached answer if all cited chunks still exist and nothing in cited repositories/spaces has changed since caching. Increments the hit counter. Returns empty result if cache validation fails.

<!-- dth:chunk 31abffd9db2f70ef -->
## `Queries.HitCachedAnswer`

Increments the hit count for a cached answer by key, used to track access frequency for cache management.

<!-- dth:chunk b3ffdc93f28df198 -->
## `InsertMessageParams`

Parameters for inserting a message (user question or assistant answer) into a conversation thread, including content, citations, LLM provider/model details, token counts, costs, and investigation metadata.

<!-- dth:chunk 7032eb0a2555a7bd -->
## `Queries.InsertMessage`

Inserts a message into a conversation thread with all metadata including role, content, citations, provider details, token usage, and whether the answer was cached or manually investigated.

<!-- dth:chunk b23b0fc8bc183815 -->
## `ListChunkPathsParams`

Parameters for listing distinct file paths containing specific text, with filters for repository scope, content sources, and result limit.

<!-- dth:chunk 0b2e9ea37a3c85d3 -->
## `ListChunkPathsRow`

Represents a distinct file path with the count of chunks it contains and its scope (repository or space).

<!-- dth:chunk 5672379475222fc1 -->
## `Queries.ListChunkPaths`

Lists distinct file paths containing the given text (case-insensitive), along with the chunk count per path, ordered by scope and path.

<!-- dth:chunk 2e96fb0a6368a9c8 -->
## `OverviewChunksParams`

Parameters for querying chunks suitable for repository overview answers, with filters for repository scope, content sources, and result limit.

<!-- dth:chunk eed6b2ede4d6ae66 -->
## `OverviewChunksRow`

A chunk ranked for repository overview context, with chunk ID and relevance rank (3 for READMEs, 2 for architecture docs, 1 for generated entry-point docs).

<!-- dth:chunk 7daec2d1271b271a -->
## `Queries.OverviewChunks`

Retrieves chunks most relevant for broad repository questions, prioritizing READMEs and architecture/overview documents, then generated entry-point docs. Results are ordered by rank and path length.

<!-- dth:chunk 06fadb77c6506f2a -->
## `PutCachedAnswerParams`

Parameters for caching an answer: the cache key, answer text, citations, and the list of chunks cited.

<!-- dth:chunk 2d9088c875a4b8d5 -->
## `Queries.PutCachedAnswer`

Stores or updates a cached answer with its citations and cited chunks, automatically deriving the scopes from chunk metadata and resetting the hit counter on update.

<!-- dth:chunk cc39f2ff3d49a74d -->
## `SetAnswerMeaningParams`

Parameters for storing semantic information about a cached answer, including the original question, vector embedding, and associated scope key for similarity matching.

<!-- dth:chunk 67dfae61332398c5 -->
## `Queries.SetAnswerMeaning`

Updates a cached answer record with semantic metadata: the original question, scope key, embedding model, and vector embedding for similarity-based retrieval.

<!-- dth:chunk ad7d818608d0dd76 -->
## `SimilarAnswerCandidatesParams`

Parameters for finding similar cached answers: identifies the scope and embedding model, with a limit on candidates returned.

<!-- dth:chunk 353b028fd9978e06 -->
## `SimilarAnswerCandidatesRow`

A candidate cached answer with its question embedding and full answer content; used for semantic similarity matching against new questions.

<!-- dth:chunk b19660b42a87ecfd -->
## `Queries.SimilarAnswerCandidates`

Retrieves fresh cached answers from the same scope with the same embedding model (newest first), used by the Hub to find semantically similar prior answers. Applies same freshness validation as GetCachedAnswer.

<!-- dth:chunk 80d55e05247b0c14 -->
## `Queries.ThreadMessages`

Retrieves all messages in a conversation thread ordered by creation time, with user questions appearing before their corresponding answers (questions and answers share the same created_at timestamp).

<!-- dth:chunk 79f17f66b91400b6 -->
## `__module__`

SQL query constants for Q&A operations: chunk retrieval by path, message storage, answer caching with freshness validation, semantic similarity lookup, and conversation thread management.

<!-- dth:generated source="internal/store/qa.go" — edit only inside dth:human blocks -->
# `internal/store/qa.go`

<!-- dth:chunk 44d5821a6faf3c1a -->
## `QA.FullText`

Performs keyword-based search with a fallback strategy: if the strict all-words search returns fewer than k/2 results, queries again for any single word and appends unique non-duplicate results. Rewords overly broad questions internally to improve coverage.

<!-- dth:chunk 12b4cb5756492149 -->
## `QA.fullText`

Executes a full-text search query for the given question within the specified scope, returning up to k results as vector hits with their relevance scores. On error, returns a descriptive error message prefixed with "full-text search:".

<!-- dth:chunk 6bdbc40861352091 -->
## `anyWords`

anyWords rewrites a question as "w1 or w2 or …" for websearch_to_tsquery (stop words drop out there).

<!-- dth:chunk 0fe597e64ec6b268 -->
## `QA.Overview`

Retrieves high-level repository material (READMEs, architecture docs, entry-point documentation) relevant to broad questions, scoped by repositories and sources, returning up to k results ranked by relevance.

<!-- dth:chunk da7114c2c3bc0e26 -->
## `QA.CachedAnswer`

Retrieves a cached answer by key if it exists and its source chunks remain unchanged. Returns empty answer and false (not found) if the key does not exist, or an error on database failure.

<!-- dth:chunk 65181582df4d69d0 -->
## `QA.PutAnswer`

Stores an answer and its list of source chunk IDs, serializing citation metadata to JSON. Normalizes citations to a list of chunk IDs for indexing.

<!-- dth:chunk 86b2ea98c3c59106 -->
## `QA.SimilarAnswer`

Finds a cached answer from a question with equivalent meaning, comparing cosine similarity of embeddings against a minimum threshold and filtering by an optional accept predicate. Returns the answer, a found flag, or error. Ignores errors when updating hit metadata.

<!-- dth:chunk 61a66853544d1a46 -->
## `QA.PutAnswerMeaning`

Caches an answer and its question's embedding together, enabling future retrieval of reworded questions. Delegates storage to PutAnswer, then records the embedding and question text for semantic matching.

<!-- dth:chunk da69791737c03d98 -->
## `Message`

Represents a single conversation turn with role, content, citations, model info, token usage (for assistant messages), cached/investigated flags, sift summary, and optional user feedback.

<!-- dth:chunk 2a477bfae4dbaec0 -->
## `QA.GetThread`

Loads a thread with its metadata and optionally its messages, enforcing ownership by userID and validating the thread ID format. Returns ErrNotFound for invalid IDs or other users' threads. Reconstructs assistant message usage stats from individual token and cost fields.

<!-- dth:chunk 23b758de12ec6519 -->
## `QA.AddExchange`

Stores a question-answer exchange within a transaction: inserts the user question, the assistant answer with citations and optional sift summary, and updates the thread timestamp. Returns the newly created assistant message ID.

<!-- dth:chunk 40844606ff61ff5c -->
## `QA.ChunksForPath`

Returns chunks from files at or ending with the given path within the scope, up to the specified limit. Trims leading slashes and whitespace from the path before querying.

<!-- dth:chunk cb8896f9dfc790d7 -->
## `QA.ListPaths`

Lists indexed file paths whose names contain the given text within the scope, limited by count. Returns repo scope, file path, and chunk count for each match. Trims whitespace from the search text.

<!-- dth:chunk 2ea8aacb46d10dc6 -->
## `__module__`

Module-level variables: wordRE regex matches sequences of at least 2 letters, digits, or underscores; similarCandidates constant sets the search width for semantic answer caching to 300 candidates.

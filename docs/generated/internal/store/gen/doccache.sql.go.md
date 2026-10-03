<!-- dth:generated source="internal/store/gen/doccache.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/doccache.sql.go`

<!-- dth:chunk 3ef2365156d09804 -->
## `Queries.DocumentedChunkIDs`

Retrieves all chunk IDs that have generated documentation sections associated with a repository. Executes a query that returns distinct chunk IDs from the doc_nodes table filtered by repo_id and where documentation exists. Returns a slice of chunk ID strings or an error if the query fails.

<!-- dth:chunk 1bb4108f7d1a5f5c -->
## `Queries.GCDocCache`

Garbage collects stale entries from the doc_cache by deleting rows where the used_at timestamp is older than the provided cutoff time. Returns the number of rows affected or an error if the operation fails.

<!-- dth:chunk 043e696a8c58f3b1 -->
## `Queries.GetAppSetting`

Retrieves the value of an application setting by key. Returns the setting value as a string or an error (including sql.ErrNoRows if the key does not exist).

<!-- dth:chunk 429b637ce87eb574 -->
## `GetDocCacheParams`

Parameters for querying cached documentation entries, containing slices of content hashes and symbol names to filter results.

<!-- dth:chunk 5a022815d535e889 -->
## `GetDocCacheRow`

Represents a cached documentation entry with its content hash, symbol name, and documentation body text.

<!-- dth:chunk 61302f350d0543d0 -->
## `Queries.GetDocCache`

Retrieves cached documentation entries matching any of the provided content hashes and symbols. Returns a slice of GetDocCacheRow results containing the content hash, symbol, and cached body text for each match, or an error if the query fails.

<!-- dth:chunk ea57ce0cf5ca3b6d -->
## `PutDocCacheParams`

Parameters for storing a documentation cache entry, including the content hash, symbol name, documentation body, and the AI model used to generate it.

<!-- dth:chunk 90e94bce952bd89a -->
## `Queries.PutDocCache`

Stores or updates a documentation cache entry. On conflict (same content_hash and symbol pair), updates the body and model fields and refreshes the used_at timestamp to track last access. Returns an error if the operation fails.

<!-- dth:chunk 9154a9370e29357f -->
## `SetAppSettingParams`

Parameters for setting an application configuration value with a key and value string pair.

<!-- dth:chunk 5d3ab5d52327489e -->
## `Queries.SetAppSetting`

Stores or updates an application setting. On conflict with an existing key, updates the value and refreshes the updated_at timestamp. Returns an error if the operation fails.

<!-- dth:chunk ca093d8034fadf7e -->
## `TouchDocCacheParams`

Parameters for updating cache access times, containing slices of content hashes and symbol names identifying the cache entries to touch.

<!-- dth:chunk 76a869fcac4a68a7 -->
## `Queries.TouchDocCache`

Updates the used_at timestamp to now() for all cached documentation entries matching the provided content hashes and symbols. Returns an error if the operation fails.

<!-- dth:chunk 752900cf86955773 -->
## `__module__`

SQL query constant definitions for documentation cache operations, including queries to retrieve documented chunk IDs, manage the doc_cache table with garbage collection and access tracking, and manage application settings persistence.

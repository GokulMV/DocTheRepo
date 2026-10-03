<!-- dth:generated source="internal/store/queries/doccache.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/doccache.sql`

<!-- dth:chunk 23ccb7f3a41cbfae -->
## `internal/store/queries/doccache.sql`

SQL query definitions for managing documentation cache and application settings. Includes:

**GetDocCache**: Retrieves cached documentation bodies for given content hash and symbol pairs.

**TouchDocCache**: Updates the `used_at` timestamp for cached entries to track access time for garbage collection.

**PutDocCache**: Inserts or updates a documentation cache entry with the generated body and model used, setting `used_at` to current time on insert or conflict.

**GCDocCache**: Deletes cache entries older than the specified cutoff timestamp, returning the count of deleted rows.

**DocumentedChunkIDs**: Retrieves all unique chunk IDs from the documentation node tree that have generated doc sections.

**GetAppSetting** and **SetAppSetting**: Simple key-value store for application configuration, with SET upserting on conflict and updating the timestamp.

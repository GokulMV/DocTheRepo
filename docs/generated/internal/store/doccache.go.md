<!-- dth:generated source="internal/store/doccache.go" — edit only inside dth:human blocks -->
# `internal/store/doccache.go`

<!-- dth:chunk 2529cfd2ae9dbf23 -->
## `DocCache`

DocCache is generated doc sections by the content they describe (docrouter.Cache).

<!-- dth:chunk d10d1c0a2d5071ed -->
## `NewDocCache`

NewDocCache returns the doc cache.

<!-- dth:chunk d676dcc042a03530 -->
## `DocCache.Get`

Retrieves cached documentation bodies for the given keys. Returns a map of keys to their cached bodies for any that exist in the cache. Accepts a context and a slice of `docrouter.Key` values (containing ContentHash and Symbol). Queries the database for matching entries and updates their access timestamps to track usage. Returns an empty map if no keys are provided or no matches are found.

<!-- dth:chunk 9e0cb4f01fbea899 -->
## `DocCache.Put`

Stores generated documentation bodies in the cache, associating them with their keys (ContentHash and Symbol) and the model used to generate them. Iterates through the provided map and inserts each body individually. Returns an error if any insert operation fails; insertion stops at the first error.

<!-- dth:chunk 6460e9b1da89ffd9 -->
## `DocCache.GC`

GC drops entries not used since cutoff.

<!-- dth:chunk 2bad5e882f6b4a5c -->
## `Docs.DocumentedChunks`

Returns a set of chunk IDs that have associated generated documentation for the given repository. Queries the database for all documented chunk IDs and converts the result to a boolean map for efficient lookup. Returns an empty map if no documented chunks exist.

<!-- dth:chunk 6e9c48286dedc466 -->
## `AppSettings`

AppSettings are hub-wide settings changed in the UI.

<!-- dth:chunk 648855d983d2a38c -->
## `NewAppSettings`

NewAppSettings returns the settings store.

<!-- dth:chunk ee79ab957fe7de4c -->
## `AppSettings.Get`

Retrieves an application setting by key from persistent storage. Returns the setting value as a string, or an empty string if the setting was never set or does not exist. Distinguishes between a missing setting (returns empty string) and a database error (returns the error).

<!-- dth:chunk f4d9d6436a011cbb -->
## `AppSettings.Set`

Set stores a setting.

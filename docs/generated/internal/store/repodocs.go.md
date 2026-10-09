<!-- dth:generated source="internal/store/repodocs.go" — edit only inside dth:human blocks -->
# `internal/store/repodocs.go`

Provides database storage and retrieval for repository documentation (Docs v2), file cards, and repository facts extracted from the knowledge graph.

<!-- dth:chunk 9080289c452f7030 -->
## `RepoDocs`

RepoDocs stores Docs v2 documents and file cards, and reads the facts they are written from.

<!-- dth:chunk b99b263c954ed47b -->
## `NewRepoDocs`

NewRepoDocs returns the store.

<!-- dth:chunk ce07ca0e456afe9e -->
## `RepoDocs.Facts`

Gathers comprehensive facts about a repository from the knowledge graph: repository metadata, files with their symbols and call relationships, and declared facts (endpoints, env vars, datastores, topics, dependencies, owners, services, and diagrams). Returns `ErrNotFound` if the repository doesn't exist.

<!-- dth:chunk 77555eab9f129332 -->
## `sortedMapKeys`

Helper that returns sorted string keys from a map, used to ensure deterministic iteration order when building file facts.

<!-- dth:chunk cf9a6ec7e8565838 -->
## `RepoDocs.Cards`

Retrieves all file cards for a repository keyed by file path, unmarshaling JSON-encoded card objects from storage.

<!-- dth:chunk d2af1ef67df05040 -->
## `RepoDocs.PutCards`

Batch-inserts or updates file cards, storing each card's JSON representation along with its shape and path. Uses PostgreSQL `ON CONFLICT` to upsert.

<!-- dth:chunk 538007a0f29473e5 -->
## `scanDoc`

Scans a single document row from the database, deserializing JSON fields (sections, gaps, why, file hashes) and converting floating-point confidence/changed values to float64. Returns `ErrNotFound` if the row doesn't exist.

<!-- dth:chunk 352bc91d12b1997c -->
## `RepoDocs.Docs`

Retrieves all documents for a repository in navigation order (by order, then title), unmarshaling each via `scanDoc`.

<!-- dth:chunk 76e22c74f1efc5ca -->
## `RepoDocs.Doc`

Doc returns one document.

<!-- dth:chunk 319951b753e8bb19 -->
## `RepoDocs.PutDoc`

Inserts or updates a document by (repo, type, key), generating a new ID and timestamp if missing. Handles system-wide documents (repo_id null) separately. Uses PostgreSQL `ON CONFLICT` to upsert, returning the resulting document state after insert/update.

<!-- dth:chunk e8502a6a91b6b786 -->
## `RepoDocs.DeleteDocsExcept`

Soft-deletes documents not in the keep list (specified as type/key pairs), returning their concatenated `type/key` identifiers. Used when regenerating repository docs to clean up stale entries.

<!-- dth:chunk 0c47a913d827f0d2 -->
## `RepoDocs.MonthCost`

Returns the total USD cost charged to a repository for document generation (`docgen`, `docgen_fast`, `decide` features) during the current calendar month.

<!-- dth:chunk aa5ab53000c43e8c -->
## `RepoDocs.DropLegacy`

Removes Docs v1 legacy generated documentation: deletes all `doc_nodes` and soft-deletes generated doc chunks (except those in `@docs/` paths), returning the deleted chunk IDs and incrementing the index version to trigger search re-indexing.

<!-- dth:chunk 2e77ece06230e94a -->
## `__module__`

SQL column selection constant for document queries, selecting all fields from `repo_docs` table with null-coalescing for nullable columns.

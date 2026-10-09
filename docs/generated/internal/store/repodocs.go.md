<!-- dth:generated source="internal/store/repodocs.go" — edit only inside dth:human blocks -->
# `internal/store/repodocs.go`

This file provides database storage operations for documentation records, including retrieval and upsert (insert/update) functionality backed by PostgreSQL.

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

Deserializes a database row into a `repodocs.Doc` struct, handling JSON unmarshaling of embedded JSON fields (Sections, Gaps, Why, FileHashes, DraftProblems) and converting float32 database values to float64 for Confidence and Changed fields. Returns `ports.ErrNotFound` if the row doesn't exist, and silently ignores JSON unmarshaling errors to tolerate malformed data.

<!-- dth:chunk 352bc91d12b1997c -->
## `RepoDocs.Docs`

Retrieves all documents for a repository in navigation order (by order, then title), unmarshaling each via `scanDoc`.

<!-- dth:chunk 76e22c74f1efc5ca -->
## `RepoDocs.Doc`

Doc returns one document.

<!-- dth:chunk 319951b753e8bb19 -->
## `RepoDocs.PutDoc`

Inserts or replaces a document by (repo, type, key) using PostgreSQL's UPSERT mechanism. Generates a new ID if absent and sets UpdatedAt to now if zero. Handles system-wide documents (repo_id = NULL) with a modified conflict clause. Converts complex fields (Sections, Gaps, Why, FileHashes, DraftProblems) to JSON, substituting empty arrays for null values. Returns the inserted/updated document by calling scanDoc on the query result.

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

Query constant listing all columns to retrieve from the repo_docs table for reconstructing a Doc record, used in RETURNING clauses. Includes fields for document identity, content (sections, gaps), metadata (type, key, title, group, order), processing results (hashes, model, tokens), and status tracking (confidence, changed, calibrated, error, status, updated_at).

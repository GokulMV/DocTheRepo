<!-- dth:generated source="internal/store/system.go" — edit only inside dth:human blocks -->
# `internal/store/system.go`

Provides database storage operations for repository documentation, including loading wiring facts and calculating inter-repository system links.

<!-- dth:chunk 58b32d9682b2cf76 -->
## `RepoDocs.SystemLinks`

Builds a list of inter-repository system links by querying four types of relationships: events (one repo publishes a topic another subscribes to), code references (calls, endpoints, services across repos), package dependencies that resolve to tracked repositories, and wiring-based links (configuration, pipelines). Deduplicates links by (fromRepo, toRepo, kind, via) key and increments a count for duplicates. Results are sorted by source repository name, then target name, then link kind. Each query is limited to 2000 results.

<!-- dth:chunk 7b9ec26d412c327a -->
## `RepoDocs.SystemDoc`

SystemDoc returns the system-wide document (ports.ErrNotFound when none).

<!-- dth:chunk c89fe5e2934d7dce -->
## `RepoDocs.DeleteSystemDoc`

DeleteSystemDoc removes it (the repositories no longer talk to each other).

<!-- dth:chunk d68e911ce16e1fcf -->
## `RepoDocs.SystemChunks`

Retrieves all stored documentation chunks for the System architecture across all repositories, including both active and deleted entries. Returns results via `ChunksAtPathAnyRepo` database query converted to the standard chunk format.

<!-- dth:chunk ec92b27a7155ac8c -->
## `RepoDocs.wiringRepos`

Loads all enabled repositories and their associated wiring facts (like configuration and pipeline references) from the database. Returns a slice of `WiringRepo` structures grouped by repository, with each repository's wires accumulated in order. Wiring facts with null kind are skipped (representing repositories with no wiring data).

<!-- dth:chunk a73a139fabb4afcd -->
## `RepoDocs.PutWiring`

Replaces a repository's wiring facts and returns whether they changed. Computes a fingerprint of wiring records (joining kind, value, and path) to detect changes efficiently. Uses a transaction to delete old wires and insert new ones for the given repo ID. Returns false if the wiring is identical to the existing record, or true if updated; propagates transaction errors.

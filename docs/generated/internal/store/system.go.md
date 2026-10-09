<!-- dth:generated source="internal/store/system.go" — edit only inside dth:human blocks -->
# `internal/store/system.go`

Provides methods to query inter-repository system architecture links and documentation chunks from the database.

<!-- dth:chunk 58b32d9682b2cf76 -->
## `RepoDocs.SystemLinks`

Fetches inter-repository system links by querying the database for three types of connections: event publish-subscribe relationships between repositories, direct code references (calls, API endpoints, library dependencies), and package dependencies matching tracked repository names. Returns a sorted slice of `SystemLink` structures deduplicating identical links while counting occurrences. Limits each query type to 2000 results and handles string matching for package names with case-insensitivity and multiple naming patterns (full paths, scoped packages, directory containment).

<!-- dth:chunk 7b9ec26d412c327a -->
## `RepoDocs.SystemDoc`

SystemDoc returns the system-wide document (ports.ErrNotFound when none).

<!-- dth:chunk c89fe5e2934d7dce -->
## `RepoDocs.DeleteSystemDoc`

DeleteSystemDoc removes it (the repositories no longer talk to each other).

<!-- dth:chunk d68e911ce16e1fcf -->
## `RepoDocs.SystemChunks`

Retrieves all stored documentation chunks for the System architecture across all repositories, including both active and deleted entries. Returns results via `ChunksAtPathAnyRepo` database query converted to the standard chunk format.

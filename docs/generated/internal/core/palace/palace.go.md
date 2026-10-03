<!-- dth:generated source="internal/core/palace/palace.go" — edit only inside dth:human blocks -->
# `internal/core/palace/palace.go`

<!-- dth:chunk c365b3026661d28c -->
## `__module__`

Defines constants for entity kinds and edge types that model a codebase as a directed graph. Kind constants (KindRepo, KindModule, etc.) represent node types spanning code artifacts (repo, module, file, symbol), infrastructure (endpoint, datastore, cloud resource), documentation (Confluence page, Jira issue, Notion page), and organizational entities (team, person). Edge constants (EdgeContains, EdgeCalls, etc.) represent directed relationships between nodes, such as containment hierarchies, function calls, dependency declarations, environment variable reads, datastore usage, pub/sub relationships, ownership, and documentation references. These constants form the vocabulary for representing and querying the dependency graph of a codebase.

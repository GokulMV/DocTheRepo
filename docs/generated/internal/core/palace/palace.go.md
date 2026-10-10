<!-- dth:generated source="internal/core/palace/palace.go" — edit only inside dth:human blocks -->
# `internal/core/palace/palace.go`

Provides a graph-based model for representing code dependencies and architectural relationships in a repository, with functions to extract and analyze these relationships.

<!-- dth:chunk 2644fb4712e52f19 -->
## `ExtractImports`

Extracts a Go source file's imports of packages within the same repository and represents them as file-to-module edges in a dependency graph. Only processes Go files with a defined `goModule` (the repository's module path from go.mod). For each import statement whose path starts with the module path, creates an edge from the file entity to a module entity representing the imported package directory. Filters out standard library and external imports (those not under the module path) and the file's own directory. Records evidence with the import's line number and commit. Returns an empty graph if the analysis is nil, not a Go file, or if goModule is unset.

<!-- dth:chunk c365b3026661d28c -->
## `__module__`

Defines entity kinds and edge types used in the dependency graph data model. `KindModule` and other entity kind constants classify nodes (repositories, files, symbols, endpoints, datastores, teams, etc.). `EdgeImports` is a newly added edge type representing file-to-module relationships within a repository, complementing existing edges like `EdgeContains`, `EdgeCalls`, and `EdgeDependsOn`.

<!-- dth:generated source="internal/store/architecture.go" — edit only inside dth:human blocks -->
# `internal/store/architecture.go`

Manages retrieval and storage of repository architecture documentation, including entities, relationships, diagrams, and external documentation links.

<!-- dth:chunk dc5b32466d52bd0d -->
## `ArchDoc`

Represents a documentation page linked to a repository or service entity. Used to display architecture documentation pages sourced from external platforms (Confluence, Jira, or Notion). The `URL` field is optional and populated when the documentation location is known; other fields identify the entity being documented and the relationship type.

<!-- dth:chunk dab0f922a2cdef39 -->
## `ArchitectureStore.Repo`

Retrieves the complete architecture for a repository, including its services, endpoints, modules, edges, and documentation pages, filtered by the caller's access scope. Returns `ErrNotFound` if the repository ID is invalid or not found in the database. Queries entities and their relationships, filters them by visibility permissions, fetches associated documentation URLs from external systems, retrieves related diagrams, and returns the timestamp of the last update. The `sc` parameter controls which repositories are visible to the caller; if `sc.All` is false, only repositories in `sc.RepoIDs` are shown.

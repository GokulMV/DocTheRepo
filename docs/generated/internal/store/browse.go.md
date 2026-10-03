<!-- dth:generated source="internal/store/browse.go" — edit only inside dth:human blocks -->
# `internal/store/browse.go`

<!-- dth:chunk 704f0c8d4696d418 -->
## `NodeDetail`

Represents a tree node with its full content and metadata. Contains the node's identity (ID, kind, title), location information (path, repo, parent), documentation (summary, markdown), and associated code chunks. Ancestors field enables UI navigation by listing node IDs from the root to the parent.

<!-- dth:chunk a27be95dbb30a784 -->
## `Browse.Node`

Loads a tree node's details with full content; for section nodes, returns the parent file's markdown content instead of the section's own. Fetches the node record, its ancestor chain for navigation, and all code chunks associated with the file (or file-section). Returns NodeDetail with chunks as a list of maps containing chunk metadata (ID, path, symbol, language, signature, commit SHA).

<!-- dth:chunk ee182557543ed454 -->
## `ShelfEntry`

Represents a single item on a library shelf, such as a documentation node, entity, or external knowledge source (Confluence, Jira, etc.). Fields include the item's type and ID, display properties (title, path, summary), ownership scope (repo_id), pinning status, user notes, and source attribution for team-sourced items.

<!-- dth:chunk 44c02d8104413177 -->
## `Browse.Shelf`

Loads a shelf and filters its items to only those readable by the caller (based on scope). Queries three sources: repo-owned documentation items, entities, and shared knowledge sources (Confluence pages, Jira issues, knowledge docs). Returns a Shelf with all accessible entries sorted and counted, or ErrNotFound if the shelf slug doesn't exist.

<!-- dth:chunk 5e94be12f94cd733 -->
## `Browse.PinItem`

Manually pins an item to a shelf; pinned items are excluded from rule-based re-evaluation. Validates that the shelf ID is a valid UUID and the item type is one of six allowed types (doc_node, entity, confluence_page, jira_issue, knowledge_doc, known_issue). Returns ValidationError if item type is invalid, or ErrNotFound if the operation fails.

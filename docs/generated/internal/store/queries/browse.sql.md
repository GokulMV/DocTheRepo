<!-- dth:generated source="internal/store/queries/browse.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/browse.sql`

<!-- dth:chunk 6dde36d03080e89b -->
## `internal/store/queries/browse.sql`

### browse.sql

This file defines SQL queries for browsing and managing documentation nodes, entities, library shelves, repository settings, connectors, and spend limits.

**GetDocNode**: Retrieves a single documentation node by ID with its associated repository name via a join on repos.

**DocChildrenWithCounts**: Returns immediate children of a doc_node within a repository, ordered by type (directories first, then files, then others), with a boolean indicating whether each child has further children.

**DocRoots**: Fetches root-level repository nodes, filtered by access control (all_repos flag or specific repo IDs), with has_children indicators.

**DocSectionChunks**: Gets code chunks associated with sections of a documentation file, ordered by definition order, excluding deleted chunks.

**ListEntities**: Queries entities with filtering by kind, name/key search patterns, repository membership, and access control, supporting pagination via after_kind/after_key cursor.

**GetEntity**: Retrieves a single non-deleted entity by ID.

**EntityEdges**: Fetches graph edges between entities with full details on both source and destination endpoints, applying ACL filtering on repository-owned entities, limited to 2000 results.

**ShelvesWithCounts**: Returns library shelves with counts of items per shelf, useful for UI display.

**GetShelfBySlug, ShelfDocItems, ShelfEntityItems**: Operations for managing and retrieving shelf contents, supporting mixed doc_node and entity items with pinning and notes.

**CreateShelf, UpdateShelf, DeleteShelf, PinShelfItem**: CRUD operations for library shelves and their item management.

**UpdateRepoSettings**: Updates repository configuration including branch tracking, docs path, push mode, approvers, and owners in a single operation.

**ListConnectorsPublic, UpdateConnector, DeleteConnector**: Manages document connectors excluding the internal 'upload' type, with credential and webhook secret handling.

**ReplaceSpendLimitsDelete, InsertSpendLimit**: Spend limit management for rate and cost control.

**CountLiveChunks, LiveChunkTokens**: Aggregate metrics on non-deleted chunks and their approximate token counts (dividing by 4).

**DocAncestors**: Recursive query returning the path from a doc node to its repository root, useful for breadcrumb navigation.

<!-- dth:generated source="web/src/pages/Library.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Library.tsx`

Component for browsing and managing documentation organized into shelves, supporting multiple document sources with role-based upload and creation permissions.

<!-- dth:chunk 05a9576549867e6b -->
## `Library`

Renders a paginated library view for browsing and managing documentation shelves. When no shelf slug is provided, displays a grid of all available shelves with item counts and optional "Curated" badges, with a toggle to show empty shelves and a list of uploaded files below. When a shelf slug is provided, displays that shelf's contents as a sortable list of items (doc nodes, uploads, Confluence pages, Jira issues, or knowledge docs), each showing type badges, pinned status, summaries, and source paths as appropriate. Editors and above can upload docs and create new shelves. Uses role-based access control via `atLeast()` to gate creation actions.

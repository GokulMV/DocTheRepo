<!-- dth:generated source="web/src/pages/Architecture.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Architecture.tsx`

Displays repository architecture graphs with extracted components, dependencies, and custom diagrams, with permissions-based scanning and node detail inspection.

<!-- dth:chunk 1dd5814cfeb5f45e -->
## `Architecture`

Represents the complete architecture data for a repository, including its dependency graph, configuration, and documentation. Contains the repository metadata, extracted nodes and links forming the architecture diagram, counts of hidden elements, environment variables, linked documentation and owners, and any authored archify diagrams. The `restricted` field tracks how many cross-repo links are not shown due to access control, and `updated_at` indicates when the architecture was last regenerated.

<!-- dth:chunk ebf54dc9cb059fd4 -->
## `RepoArchitecture`

Displays the architecture view for a single repository, fetching architecture data and allowing authorized users to scan for and update archify diagrams. Renders a tabbed interface with a generated architecture diagram (showing nodes, links, and legend) and custom diagram tabs; when a node is selected, displays a detail card with its metadata, related links, and navigation options. Handles three key mutations: scanning the repository for new diagrams (with file-level error reporting), invalidating queries on success, and navigating to related architectures or queries. Shows loading states, error messages, and conditionally disables the scan button based on user permissions.

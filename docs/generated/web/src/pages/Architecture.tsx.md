<!-- dth:generated source="web/src/pages/Architecture.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Architecture.tsx`

<!-- dth:chunk 1dd5814cfeb5f45e -->
## `Architecture`

Data structure representing a repository's architecture, including the repository metadata, graph nodes and links, hidden node counts, optional aggregated counts, restricted external dependencies, environment variables, documentation references, owners, and authored diagrams with their last update timestamp.

<!-- dth:chunk df174620df9793da -->
## `ArchSummary`

Generates a one-sentence summary of an architecture diagram, describing what the repository exposes, depends on, and stores. Extracts endpoint groups, HTTP endpoints, messaging topics, datastores, external repository dependencies, and library usage from the architecture nodes, then formats them into human-readable prose with proper pluralization. Returns null if no notable components are found.

<!-- dth:chunk ebf54dc9cb059fd4 -->
## `RepoArchitecture`

Page component displaying a repository's architecture with switchable views between generated graph and authored diagrams. Fetches architecture data and allows authorized users to scan for new archify diagrams; displays the generated architecture as an interactive network with an optional detail panel for selected nodes, plus configuration and documentation sections. Handles loading, error states, and shows results of the diagram scan operation including any read failures.

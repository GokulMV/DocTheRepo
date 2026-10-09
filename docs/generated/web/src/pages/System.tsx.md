<!-- dth:generated source="web/src/pages/System.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/System.tsx`

A React page component displaying the system architecture across multiple repositories, including auto-generated diagrams, documentation, and a table of inter-repository connections detected from code analysis.

<!-- dth:chunk 4934c1514b528ad8 -->
## `codeURL`

Generates a GitHub or GitLab URL pointing to a specific line in a repository's code. Given a repository name, file path, and line number, finds the matching repository, uses its last processed SHA or default branch as the ref, and constructs the appropriate source control URL based on the connector type. Returns `undefined` if the repository is not found or the connector type is neither GitHub nor GitLab.

<!-- dth:chunk 6f5f1b9bf79acb9d -->
## `System`

### System

A page component displaying the system architecture across repositories, including an auto-generated diagram, write-up, and table of inter-repository links. Fetches system metadata from `/system` endpoint with automatic refetching every 3 seconds when a write job is queued or processing. Admins can trigger a system write with the "Write again" button. The page renders repository-qualified citations (owner/repo/path:line) as clickable code links using `codeURL`. Displays different content states: a mermaid diagram of the system, an "at a glance" summary with confidence metadata, detailed sections from the write-up (if complete and readable), and a table of detected links with kind badges (event, api, call, library, image, pipeline, other) and source locations.

<!-- dth:chunk 03aa3d77bde79f68 -->
## `__module__`

### KIND

A mapping of link kind identifiers to human-readable display labels used in the links table (e.g., 'event' → 'Event', 'api' → 'API call'). Used with Badge component to color-code links by their connection type.

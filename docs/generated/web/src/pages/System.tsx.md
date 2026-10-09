<!-- dth:generated source="web/src/pages/System.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/System.tsx`

React component that visualizes how repositories in a codebase interconnect through APIs, events, and packages, with generated documentation and an interactive link map.

<!-- dth:chunk 4934c1514b528ad8 -->
## `codeURL`

Generates a GitHub or GitLab URL pointing to a specific line in a repository's code. Given a repository name, file path, and line number, finds the matching repository, uses its last processed SHA or default branch as the ref, and constructs the appropriate source control URL based on the connector type. Returns `undefined` if the repository is not found or the connector type is neither GitHub nor GitLab.

<!-- dth:chunk 6f5f1b9bf79acb9d -->
## `System`

Displays the system architecture view showing how repositories interconnect through APIs, events, and packages. Fetches system data with auto-refresh during active processing jobs, displays a Mermaid diagram of repository relationships, renders generated documentation with confidence badges and timestamps, and shows a table of discovered inter-repository links with source code locations. Admins can trigger regeneration of the architecture write-up via a button that becomes available when links exist. Citations in documentation are converted to clickable source code links via `codeURL`.

<!-- dth:chunk 03aa3d77bde79f68 -->
## `__module__`

Maps internal link kind identifiers to human-readable labels for display in the system architecture links table: 'event' → 'Event', 'api' → 'API call', 'call' → 'Code call', 'library' → 'Package', 'other' → 'Link'.

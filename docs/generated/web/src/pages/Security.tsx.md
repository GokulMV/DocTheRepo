<!-- dth:generated source="web/src/pages/Security.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Security.tsx`

Security scanning and vulnerability management page, displaying repository scan results and handling user-selected finding fixes via generated pull requests.

<!-- dth:chunk 40882f1a7a1fbb0e -->
## `SecurityModule`

An available security scanning module that can be selected when creating a new scan. Includes its name, description, whether it requires a running application, and whether it is enabled by default. Static modules analyze code without runtime interaction; non-static modules need a running app and may be disabled with a reason.

<!-- dth:chunk a341dbc8a29abb56 -->
## `Fix`

Describes the fix applied to a security finding, including affected files and tests, plus a risk assessment. Used by the Finding interface to track what changes were made when addressing a vulnerability.

<!-- dth:chunk 72fc89d0ffc2b0ff -->
## `Finding`

A discovered security vulnerability with its location, details, evidence, and status. Includes the code location (file and line range), reproduction steps, severity assessment, and optional fix information (PR URL or error). Status tracks validation (confirmed/plausible/rejected) and fix_status tracks remediation progress.

<!-- dth:chunk 03280644fa03faa6 -->
## `Scan`

A completed or in-progress security scan of a repository. Contains the scan status (queued/running/done), commit scanned, selected modules, verdict (GO/NO-GO), summary with counts of findings by priority level, and optionally findings and error details. Supports tracking cached module results for unchanged code.

<!-- dth:chunk 9812f2b59de3d9bc -->
## `Plan`

Estimates the scope and cost of a planned scan before execution. Lists the modules to be used, files to be read per module, total file count, and estimated token cost for the Hub's security route. May include cached modules from prior scans.

<!-- dth:chunk ae71b181636271fb -->
## `running`

Checks if a scan is actively running, returning true if status is 'queued' or 'running'. Used to determine whether to show loading states and auto-refresh UI components.

<!-- dth:chunk f2a0cc6dc60ac174 -->
## `fixable`

Determines if a finding can be fixed, excluding rejected findings and those already queued or with an open pull request. Used to enable/disable the fix selection checkbox in the UI.

<!-- dth:chunk 50ce477003d4dbf6 -->
## `Verdict`

Displays the security verdict badge for a scan. Shows 'Not scanned' when missing, 'Queued'/'Scanning…' while running, the scan status if incomplete, or the final verdict (GO in green, NO-GO in red).

<!-- dth:chunk f33447ba7ff1edc5 -->
## `Counts`

Displays a set of badges showing the count of findings per priority level (P0–P3) from the scan summary. Shows 'No findings' message when the scan is done but has no findings across all priority levels.

<!-- dth:chunk e4c7075b2807f92a -->
## `About`

Attribution and explanation of the security scanning process. Documents that modules come from the kryptonite project (MIT-licensed), scans are static (never contact running systems), findings are double-checked and prioritized, and changes only occur after explicit user action to fix via pull request.

<!-- dth:chunk 5ee66c3a4dad1dde -->
## `Security`

Main entry point for the Security page. Routes to either the overview of all repositories' security status (if no specific repo is selected) or a detailed view of a single repository's scans and findings.

<!-- dth:chunk 1454c450acb43a1e -->
## `Overview`

Displays a table of all tracked repositories with their latest security scan status. Shows each repository's verdict, open finding counts by priority, and last scan time. Auto-refreshes when scans are in progress. Handles empty state when no repositories are tracked.

<!-- dth:chunk d925477c35f70e63 -->
## `NewScan`

Dialog for starting a new security scan on a repository. Users select modules, optionally toggle between static and dynamic modules, then view a cost estimate (files to read, token usage, cached modules) before confirming. Communicates with the backend plan and scan endpoints to control scan initialization.

<!-- dth:chunk 54d1fee989292adc -->
## `FindingRow`

Table row displaying a single security finding with priority badge, title, file location, and verification status. Expandable to show full details including code surface, reproduction steps, evidence, fix risk, and error messages. Checkbox disabled for rejected findings or those already being fixed.

<!-- dth:chunk 2b1b67464329d338 -->
## `RepoSecurity`

Shows findings from security scans for a single repository. Fetches scans list, allows selecting a scan to view, displays findings in a table, and manages bulk fixing workflow. Auto-refreshes scan while running or while fixes are pending. Filters rejected findings unless explicitly shown and opens a confirmation dialog before submitting fixes.

<!-- dth:chunk 45ac8fa6c43c37a8 -->
## `__module__`

Maps priority levels (P0, P1, P2, P3) to UI tone values (red, amber, blue, gray) for consistent badge coloring throughout the security page.

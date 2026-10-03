<!-- dth:generated source="internal/api/github_connect.go" — edit only inside dth:human blocks -->
# `internal/api/github_connect.go`

<!-- dth:chunk 5f71419ce944026b -->
## `GitHubConnectDeps`

Dependencies for the GitHub App manifest creation flow. Fields manage authentication (`Auth`), connector storage (`Connectors`), cryptographic sealing of flow state between redirects (`Seal`/`Open`), external visibility (`PublicURL`), caching (`InvalidateHost`), credential encryption (`SealKeys` with `RequireSealed` enforcement), and HTTP requests. The `Seal`/`Open` pair encrypts and decrypts transient state with authenticated associated data to prevent tampering or partial-flow corruption.

<!-- dth:chunk 7e835709cb217674 -->
## `GitHubConnectRoutes`

Registers three GitHub App connection endpoints under `/github/connect`: POST `/github/connect` returns a manifest and GitHub.com action URL for the user; GET `/github/connect/start` renders an HTML form that auto-posts the manifest to GitHub; GET `/github/connect/callback` receives the created app credentials and stores them; GET `/github/connect/setup` records the installation ID after the user installs the app. Also supports POST `/github/connect/existing` to connect an already-existing App by ID and key, and POST `/github/connect/existing/{id}/refresh` to detect newly installed instances. State flows through sealed tokens valid for 30 minutes, user-verified and expiry-checked. Redirects on success or error back to `/connectors` with status parameters.

<!-- dth:chunk 527cfe4b53270543 -->
## `appName`

Generates a GitHub App name (max 34 chars) by combining "DocTheRepo", an optional organization or hostname slug, and a random 3-byte hex suffix. Removes non-slug characters and trims hyphens. The random suffix ensures uniqueness across GitHub, where undeleted apps retain their names indefinitely. Hyphens are used instead of spaces because GitHub truncates manifest names at the first space.

<!-- dth:chunk 4f2a4b63506e3fa8 -->
## `__module__`

Package-level constants and templates: `ghStateAAD` is authenticated associated data for sealing flow state; `connectPage` is an HTML template that auto-submits a manifest form to GitHub with CSP protection; `ghLogin`, `ghCode`, `ghDigits` validate GitHub organization logins, authorization codes, and installation IDs respectively; `nonSlug` removes non-alphanumeric and non-hyphen characters for sanitizing app names.

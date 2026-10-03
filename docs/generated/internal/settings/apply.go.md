<!-- dth:generated source="internal/settings/apply.go" — edit only inside dth:human blocks -->
# `internal/settings/apply.go`

<!-- dth:chunk f9edf7eca468e9e8 -->
## `Change`

A single entry in a plan or apply report describing a configuration change. It records what kind of resource changed (auth, user, provider, connector, route, repo, or spend), its name, the action taken (create, update, unchanged, or failed), which fields were modified (if any), and optional details—never containing secret values.

<!-- dth:chunk 37e0e9eebd2d37ba -->
## `Apply`

Applies a Document to the Hub by creating missing resources and updating differing ones, following a strict dependency order: sign-in settings, users, providers, connectors, routes, repositories, then spend limits. Never deletes resources. Stops at the first error, leaving partially applied state; re-running is safe. Returns a Result with all changes made (or that would be made if dryRun is true), or an error if the operation fails.

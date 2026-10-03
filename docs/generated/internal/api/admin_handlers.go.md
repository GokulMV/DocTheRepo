<!-- dth:generated source="internal/api/admin_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/admin_handlers.go`

<!-- dth:chunk fe32afb5b3fd9a06 -->
## `AdminDeps`

Dependencies injected into admin handlers for managing repositories, connectors, providers, routing, and documentation. Includes functions for provider testing, host adapters, webhook registration, documentation generation, and settings. `SealKeys` enables unsealing of secrets; `RequireSealed` enforces sealed-only secrets. `DefaultDocMode` provides the fallback documentation strategy.

<!-- dth:chunk 8fa07af10ad8af99 -->
## `adminHandlers.openSecret`

openSecret replaces a sealed secret with its value, in place; plain values pass unless sealing is required.

<!-- dth:chunk e50915892469edcd -->
## `openSealed`

Decrypts a sealed secret or passes through an unsealed value. Checks if the value is sealed using `secrets.IsSealed()`; if sealed, uses the provided `SealKeys` to unseal it with the given purpose. Returns validation errors if sealing is required but the value is unsealed, if sealing is unavailable, or if unsealing fails. Modifies the input string in place with the decrypted plaintext.

<!-- dth:chunk 030081226986729c -->
## `AdminRoutes`

Mounts admin API routes (section 7.6) with role-based access control. Viewer role can list repositories; Editor role can dry-run code pushes; Admin role manages repositories, connectors, providers, routes, and documentation modes; Owner role can reindex. Includes routes for generating docs, syncing connectors, managing doc modes, and configuring spend limits.

<!-- dth:chunk 88c526fc19e02e94 -->
## `adminHandlers.createRepo`

HTTP handler that creates or updates a tracked repository. Validates connector and repository name are present, validates docs_path format (relative, ending with /, no parent directory references). Fetches the default branch from the host if needed. Enqueues immediate documentation generation if available, and registers a webhook for push notifications. Returns HTTP 201 with repository ID, optional job ID, and webhook registration status.

<!-- dth:chunk 05dc485bd3d2817c -->
## `adminHandlers.generateDocs`

HTTP handler that queues documentation generation for every file in a repository. Verifies the repository exists, checks that a docgen route is configured, then calls `GenerateDocs` to enqueue a full code push with reason "requested". Returns HTTP 202 with the job ID. Used to backfill docs for repositories synced before documentation generation was available.

<!-- dth:chunk 1139e217b2784193 -->
## `adminHandlers.syncConnector`

HTTP handler that enqueues push jobs for all tracked repositories of a connector. For knowledge connectors (Confluence, Jira, Notion), enqueues a single knowledge sync job. For code connectors, checks each tracked repository's branch head against the stored SHA and enqueues code push jobs for those with new commits. Returns HTTP 202 with job IDs and audits the sync.

<!-- dth:chunk 7dab4fb9a6dda094 -->
## `adminHandlers.putRoute`

HTTP handler that configures an LLM route (e.g., docgen, QA, security) by associating a feature with a provider, model, and parameters. Validates effort levels, rejects decision-only providers (jev) for non-decision features, and sets default token budgets. For docgen routes, automatically generates docs for repositories synced before docgen was routed. Returns HTTP 200 with the feature and count of queued documentation jobs.

<!-- dth:chunk 678923fa4d90b00d -->
## `AppSettings`

AppSettings stores hub-wide settings changed in the UI.

<!-- dth:chunk 621fda5fd0c4b695 -->
## `adminHandlers.docMode`

Retrieves the current documentation generation mode (thorough, balanced, or economy) by checking the settings store first, falling back to `DefaultDocMode` if unavailable or invalid. Returns both the mode and its source ("settings" or "default") for audit purposes.

<!-- dth:chunk 00f289603ab2b89f -->
## `adminHandlers.getDocMode`

HTTP handler that returns the current documentation generation mode, its source, and the list of available modes as JSON.

<!-- dth:chunk dfe6811bba1656c9 -->
## `adminHandlers.putDocMode`

HTTP handler that updates the documentation generation mode after validating it against allowed values (thorough, balanced, economy). Persists the new mode to settings and audits the change. Returns HTTP 501 if settings are unavailable, otherwise responds with the new mode and source via `getDocMode`.

<!-- dth:chunk 4dc921a42dc26636 -->
## `__module__`

Module-level constants defining the supported connector types, LLM feature names, and documentation generation modes. `DocModeKey` is the settings key for the generation mode preference; `docModes` are the valid values: thorough, balanced, and economy.

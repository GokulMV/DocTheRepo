<!-- dth:generated source="web/src/api/types.ts" — edit only inside dth:human blocks -->
# `web/src/api/types.ts`

Defines TypeScript types and interfaces for API request and response contracts.

<!-- dth:chunk 43c36371bf7c7ac9 -->
## `Me`

Represents the authenticated user's profile and permissions. Contains the user's identity (`id`, `email`, `name`), their `role` for authorization, optional feature flags scoped to the current Hub (e.g., `issues` indicates an alert source or issue exists), repository access level (either all repos or a specific list via `repo_ids`), and an optional CSRF token for form submissions.

<!-- dth:generated source="internal/api/auth_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/auth_handlers.go`

This file implements HTTP request handlers for authentication operations including user profile retrieval, login, and credential management.

<!-- dth:chunk 1e58cf3581fd9fba -->
## `authHandlers`

Core handler for authentication-related HTTP endpoints. Manages authentication service integration, security configuration (sealed client secrets, secure cookies), single sign-on via `publicURL`, optional email delivery via `mailer`, and environment-specific features. Tracks per-IP password attempt rate limits to prevent brute force attacks. Conditionally enables Issues and DocsV2/System features based on deployment configuration.

<!-- dth:chunk 3f71fe46ef4dd3d9 -->
## `authHandlers.me`

Endpoint returning authenticated user's profile and capabilities. Retrieves the current principal from context, queries repository scope (all repos or specific `repo_ids`), and returns a JSON object with user identity (`id`, `email`, `name`, `role`), repository access details, and enabled features (`issues`, `docs_v2`, `system`). For session-based auth, includes the CSRF token to prevent cross-site attacks. Returns HTTP 200 with the profile or an error response.

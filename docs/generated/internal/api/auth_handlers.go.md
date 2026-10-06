<!-- dth:generated source="internal/api/auth_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/auth_handlers.go`

HTTP handlers for authentication endpoints including login, logout, SSO callbacks, user profile, and password management.

<!-- dth:chunk 1e58cf3581fd9fba -->
## `authHandlers`

HTTP handlers for authentication operations. The struct holds the auth service, security configuration (sealed client secrets via `sealKeys`, `requireSealed` flag, `secure` cookie flag), external identity provider integration via `publicURL`, optional email delivery via `mailer`, and feature flags (`hasIssues`, `environment` for UI banner). It also maintains rate limiting per client IP to throttle password login attempts.

<!-- dth:chunk 3f71fe46ef4dd3d9 -->
## `authHandlers.me`

Responds with the authenticated user's profile and permissions. Extracts the principal from context, retrieves their repository access scope via the auth service, and returns a JSON object containing user ID, email, name, role, repository access (all repos or specific IDs), enabled features (like issues), and CSRF token (only for session-based auth). Returns HTTP 200 on success or an error response if scope retrieval fails.

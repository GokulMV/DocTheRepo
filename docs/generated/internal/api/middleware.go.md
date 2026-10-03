<!-- dth:generated source="internal/api/middleware.go" — edit only inside dth:human blocks -->
# `internal/api/middleware.go`

<!-- dth:chunk ad1b0b1356d397c5 -->
## `authenticate`

Returns an HTTP middleware that authenticates requests using bearer tokens (PATs) or session cookies. System calls (with `auth.SystemPrincipal`) bypass authentication. For bearer tokens, the `TokenPrincipal` service method is called and authentication errors are written immediately; for session cookies, `SessionPrincipal` is called and unauthenticated errors are silently ignored. If a principal is present and the request is session-authenticated, CSRF validation is enforced for mutations by comparing the `CSRFHeader` header to the principal's CSRF token using constant-time comparison. Authenticated requests continue with the principal and user ID added to the request context for logging; unauthenticated requests proceed without a principal.

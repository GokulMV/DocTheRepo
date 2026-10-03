<!-- dth:generated source="internal/api/errors.go" — edit only inside dth:human blocks -->
# `internal/api/errors.go`

<!-- dth:chunk d0faef83de7e4514 -->
## `WriteErr`

Converts domain-specific errors into HTTP responses with appropriate status codes and error codes. Uses type assertion to distinguish `paramError`, `ValidationError`, `SpendBlockedError`, `TransientError`, and sentinel errors (`ErrNotFound`, `ErrConflict`), mapping each to its corresponding HTTP status (400, 402, 404, 409, 503) and error code. Unknown errors are logged as internal errors with context correlation and returned to the client as generic 500 responses without exposing details.

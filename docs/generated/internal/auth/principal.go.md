<!-- dth:generated source="internal/auth/principal.go" — edit only inside dth:human blocks -->
# `internal/auth/principal.go`

<!-- dth:chunk e47fe34ae20bcdec -->
## `Principal`

Represents an authenticated user or system principal with identity, contact, authorization level, and authentication method details.

Fields:
- `UserID`, `Email`, `Name`: User identity and contact information (JSON-serialized)
- `Role`: Authorization level; each role includes permissions of preceding roles
- `Via`: Authentication method—"session" (cookie-based), "token" (credential-based), or "system" (the Hub itself); not JSON-serialized
- `CSRF`: Session CSRF token for cookie-authenticated mutations; not serialized
- `SessionID`: Session identifier; not serialized

<!-- dth:chunk 4604be9feb627855 -->
## `SystemPrincipal`

SystemPrincipal acts with the owner role for the Hub itself (the settings file applied at startup). It exists only inside the process: requests from the network can never carry it.

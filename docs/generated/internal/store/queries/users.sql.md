<!-- dth:generated source="internal/store/queries/users.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/users.sql`

<!-- dth:chunk b6d7de1bb789ce06 -->
## `internal/store/queries/users.sql`

A SQL query file containing user authentication, session, token, group, and audit log operations for DocTheRepo.

**User Management**: Create, retrieve, update, and delete users; count active owners; link OIDC subjects; manage passwords and login timestamps.

**Invitations**: Create user invites with expiration, retrieve invite details with user info, mark invites as used (retiring all open invites for a user), and query pending invitations.

**Authentication Settings**: Store and retrieve OIDC and password authentication configuration via upsert.

**Sessions**: Create, retrieve, update idle time, delete individual or user sessions, and garbage-collect expired/idle sessions.

**API Tokens**: Create, retrieve by hash, record usage, list user tokens, and delete tokens by ID and owner.

**Group Membership**: Create groups, manage user group memberships, and clear all group assignments.

**Repository Access**: Grant/revoke direct repo access, query accessible repos (direct or via IdP groups), and manage access levels.

**Audit Logging**: Insert audit events and list filtered audit logs by actor, action, and timestamp range.

Uses sqlc for type-safe query compilation with named parameters.

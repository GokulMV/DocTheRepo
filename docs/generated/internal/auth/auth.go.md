<!-- dth:generated source="internal/auth/auth.go" — edit only inside dth:human blocks -->
# `internal/auth/auth.go`

<!-- dth:chunk e8f871aea677e235 -->
## `Service`

Service manages users, sessions, tokens, and authorization. It holds a store reference, auth configuration, and an atomic pointer to the live OIDC client (which may differ from the config file's OIDC settings). The `Now` field allows time mocking for testing. The `Box` sealer encrypts OIDC client secrets in the UI.

<!-- dth:chunk f9f2d38b4ca2222b -->
## `User`

User represents a user as exposed by the API. It includes identity information (ID, email, name), authorization details (role, disabled status), SSO indicator, password presence, and timestamps. The `InvitePending` field (set by ListUsers) signals whether an unused, unexpired invite or reset link exists.

<!-- dth:chunk 41434e38399400d7 -->
## `toUser`

Converts a database user record to the API User representation. It extracts SSO status from the presence of an OIDC subject and password existence from the presence of a password hash.

<!-- dth:chunk 65ae766fee07d4de -->
## `Service.UpsertOIDCUser`

UpsertOIDCUser handles sign-in for identity provider users. It validates OIDC claims (subject and email required, email must be verified if stated), enforces email domain allow-list restrictions, creates new users as viewers (except the first user becomes owner), links OIDC identity to existing accounts, syncs IdP groups for repo access, and rejects disabled accounts. Updates the user's last login time and name.

<!-- dth:chunk 2bddb61bfcf9043d -->
## `Service.ListUsers`

Retrieves a paginated list of users ordered by email. Sets the `InvitePending` flag on users that have pending invites or reset links by cross-referencing against the pending invites table.

<!-- dth:chunk 5d4db1383ea44a0a -->
## `Service.UpdateUser`

Updates a user's name, role, or disabled status with validation: only owners may grant or revoke owner role, and the last active owner cannot be demoted or disabled. Names longer than 200 characters are rejected. Disabling a user immediately terminates all their sessions.

<!-- dth:chunk f651ffd7ffab5cd5 -->
## `Service.Audit`

Records an admin action audit entry. Marshals the details to JSON (defaulting to `{}` on error or nil), includes the actor's user ID if available, and appends the actor's name to details when the action originated from the system. Secrets must never be included in details.

<!-- dth:chunk 7adca7cc8cdadc54 -->
## `__module__`

Module-level errors for authentication and authorization failures. ErrLastOwner prevents the last active owner from being demoted, disabled, or removed. ErrInviteInvalid indicates an invalid, expired, or already-used invite/reset link. DummyHash is a pre-computed password hash used for timing-safe dummy verification to prevent email enumeration attacks.

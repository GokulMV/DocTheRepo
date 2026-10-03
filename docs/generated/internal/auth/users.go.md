<!-- dth:generated source="internal/auth/users.go" — edit only inside dth:human blocks -->
# `internal/auth/users.go`

<!-- dth:chunk 89a89e0785f9df72 -->
## `Service.CreateUser`

Adds a new user identified by email, with an optional display name and role. Only owners may create owner-level users. The email must be valid and not already registered; the name is capped at 200 characters. The user signs in via SSO (matched by email) or by accepting an invite link to set a password.

<!-- dth:chunk ac53b4cc192bb386 -->
## `Service.CreateInvite`

Generates a one-time invite link for setting a new password (initial or reset). Returns the raw 32-character token (shown once) and its expiration time; only the hash is persisted. Admins cannot reset owner passwords. Disabled users cannot receive invites. Prior unused invites remain valid until any invite for that user is redeemed.

<!-- dth:chunk 5cd5afecba37c320 -->
## `InviteInfo`

Metadata about an invite shown on the set-password page: the target user's email and name, and when the link expires.

<!-- dth:chunk b938c17bfde302ad -->
## `Service.invite`

Internal helper that validates an invite token: hashes the raw token, checks existence, and rejects it if already used, expired, or its user is disabled. Returns an error if the token is invalid or missing.

<!-- dth:chunk 0c3ef7a5f946befe -->
## `Service.Invite`

Validates and retrieves invite metadata without consuming the link, allowing password-setting pages to display user information and expiration time before submission.

<!-- dth:chunk 44cc4c15e05255bc -->
## `Service.AcceptInvite`

Sets the user's password from a valid invite link and retires all their open invites in a transaction. Hashes the password, invalidates all sessions (forcing re-login elsewhere), and updates the last-login timestamp. Rejects invalid or expired invites.

<!-- dth:chunk 4ddad0385fc70377 -->
## `Service.DeleteUser`

Removes a user, their sessions, tokens, questions, and grants; audit logs retain the action but anonymize the actor. Cannot remove oneself; only owners may remove owners; the system must retain at least one active owner.

<!-- dth:chunk 40991f7315618721 -->
## `Service.PasswordEnabled`

Reports whether password sign-in is enabled: defers to the auth settings if explicitly configured by an owner, otherwise enables in local mode and disables in OIDC mode.

<!-- dth:chunk 9dbed13751945e7f -->
## `__module__`

Invite links expire after 7 days.

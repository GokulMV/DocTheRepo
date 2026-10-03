<!-- dth:generated source="internal/api/auth_handlers.go" — edit only inside dth:human blocks -->
# `internal/api/auth_handlers.go`

<!-- dth:chunk 1e58cf3581fd9fba -->
## `authHandlers`

HTTP handler group for authentication operations. Manages user accounts, sessions, and sign-in methods (password and OIDC). Uses a rate limiter per IP for password attempts and supports optional email delivery of invite/password links via a `Mailer`. Seals OIDC client secrets when configured.

<!-- dth:chunk 544a1910eea5a492 -->
## `authHandlers.routes`

Registers authentication routes: public endpoints for sign-in flow (login, callback, invite acceptance); viewer endpoints for personal tokens; admin endpoints for user and invite management; owner endpoints for auth settings and email testing.

<!-- dth:chunk 9b118e42cfd4d794 -->
## `authHandlers.config`

config tells the login page which sign-in methods exist (public).

<!-- dth:chunk d4067a8c28dd1f37 -->
## `authHandlers.login`

Initiates OIDC sign-in flow: creates a login state with optional return URL, stores it in a secure cookie, and redirects to the identity provider's auth URL. Fails if OIDC is not configured. Returns HTTP 302.

<!-- dth:chunk b47366eea3db562f -->
## `authHandlers.callback`

Completes OIDC sign-in: validates login state from cookie against the state query param, exchanges the auth code for ID token claims, upserts the user, creates a session, and redirects to the original return URL. Returns HTTP 302 on success.

<!-- dth:chunk ab6276bf32f2ea78 -->
## `authHandlers.localLogin`

Password-based local sign-in: authenticates the user with email and password, rate-limited per IP, creates and returns a session with CSRF token. Fails if password login is disabled. Logs successful login to audit trail.

<!-- dth:chunk be2ecf69d32e3d71 -->
## `authHandlers.updateUser`

Patches a user's name, role, and/or disabled status (all fields optional). Validates role format before updating. Logs the change with the update parameters. Requires admin role.

<!-- dth:chunk 24e170fa64b83cda -->
## `invitePath`

invitePath is where the set-password page lives in the UI.

<!-- dth:chunk 164daba4cb98a562 -->
## `authHandlers.createUser`

Creates a new user with the specified email, name, and role (defaults to Viewer). Optionally generates an invite token and emails it if password login is enabled. Returns the created user and invite details (path and expiration) in the response. Requires admin role.

<!-- dth:chunk db6b807e27869565 -->
## `authHandlers.createInvite`

Generates a new password-reset invite token for an existing user. Returns the invite path and expiration time; fails if password login is disabled. Attempts to email the invite link to the user if their record is found. Requires admin role.

<!-- dth:chunk 75417e2dea434024 -->
## `authHandlers.deleteUser`

Deletes a user by ID. Retrieves the user's email before deletion for audit purposes. Returns HTTP 204 No Content on success. Requires admin role.

<!-- dth:chunk 8ad0354e5611e09d -->
## `authHandlers.getInvite`

Public endpoint (requires only the invite token as credential) that returns invite details: the target's email, name, and expiration time, plus flags for password and SSO availability. Supports the set-password UI page.

<!-- dth:chunk 955d58e62d8872bb -->
## `authHandlers.acceptInvite`

Accepts an invite by setting the user's password, then creates and returns a session with CSRF token. Rate-limited per client IP. Fails if password login is disabled. Logs the password-set action in the audit trail and signs the user in.

<!-- dth:chunk f84a75be0783331f -->
## `writeInviteErr`

Helper that writes invite-specific errors: returns HTTP 410 Gone for invalid or expired invites, otherwise delegates to the generic error handler.

<!-- dth:chunk b5f28c0670825c4b -->
## `authHandlers.callbackURL`

callbackURL is the single sign-on redirect URL to register with the identity provider.

<!-- dth:chunk cfee0e37cc220e7b -->
## `authHandlers.getSignIn`

Returns the current authentication configuration state and the SSO callback URL. Used by the sign-in page to determine available login methods. Requires owner role.

<!-- dth:chunk d7e89a28fa242924 -->
## `authHandlers.putSignIn`

Updates authentication settings: enables/disables password login and/or configures SSO (OIDC). Unseals the client secret using the hub's seal keys, saves settings, and audits the change. Returns the updated config. Requires owner role.

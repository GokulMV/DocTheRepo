<!-- dth:generated source="internal/api/email.go" — edit only inside dth:human blocks -->
# `internal/api/email.go`

<!-- dth:chunk 623d07e22ffcc56c -->
## `Mailer`

Mailer is an interface for sending emails, typically configured with SMTP settings. When nil, the system disables email functionality and only displays invitation links in the admin UI for manual sharing. Implementations must provide the sender's email address and SMTP server details.

<!-- dth:chunk b5ff99c7f7000b83 -->
## `authHandlers.baseURL`

Returns the Hub's external base URL for generating email links. Uses the configured `publicURL` if set (trailing slashes removed), otherwise derives it from the incoming request using HTTPS if the connection is encrypted or the `X-Forwarded-Proto` header indicates HTTPS, and HTTP otherwise.

<!-- dth:chunk 5bf54f1af7470fe9 -->
## `authHandlers.emailLink`

Sends a set-password or invitation email to a user with an expiring link. For existing users, sends a password-reset email; for new users, sends an invitation. Records the outcome in `out`: sets `emailed=true` on success, or `emailed=false` and `email_error` on failure. Does nothing if no mailer is configured. Uses the principal's name if available, falling back to email; shows "An administrator" for system-initiated actions.

<!-- dth:chunk 2ea3bca09c5e4e17 -->
## `nameOr`

Returns the user's display name if set, otherwise returns their email address.

<!-- dth:chunk 56fae6fb93572e1b -->
## `authHandlers.testEmail`

HTTP handler that sends a test email to the signed-in user to verify SMTP configuration. Requires email to be configured and the user to have an email address. Returns the recipient, sender, and SMTP server in the response on success. Responds with descriptive errors if email is not configured, the user has no email, or sending fails.

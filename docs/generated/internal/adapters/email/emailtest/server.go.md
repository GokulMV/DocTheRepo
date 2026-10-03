<!-- dth:generated source="internal/adapters/email/emailtest/server.go" — edit only inside dth:human blocks -->
# `internal/adapters/email/emailtest/server.go`

<!-- dth:chunk 983ce4ad53c3b778 -->
## `Mail`

Represents a single received SMTP message with sender, recipient, message data (headers and body with dot-unstuffing and CRLF line endings applied), and the authenticated user if AUTH PLAIN was used.

<!-- dth:chunk 661a3731dd7bebad -->
## `Server`

A minimal fake SMTP server for testing email functionality. Supports optional AUTH PLAIN authentication (when `User` and `Pass` are set) and optional RCPT TO rejection (when `Reject` is set). Maintains a thread-safe log of received messages. Clients must connect with `?starttls=off` since the server does not offer STARTTLS.

<!-- dth:chunk 108ee7441c1aaefe -->
## `Start`

Starts a fake SMTP server on a random available port, returning the server instance. Automatically closes the listener when the test ends. Accepts concurrent connections, each handled in a goroutine by the `serve` method.

<!-- dth:chunk e886bb6b7395f1b3 -->
## `Server.URL`

Returns the `smtp://` URL for connecting to the server, including authentication credentials (user:pass@) if configured, with `?starttls=off` appended.

<!-- dth:chunk f548574786f5e311 -->
## `Server.Mail`

Returns a snapshot of all received messages, thread-safely copying the internal mail slice to prevent concurrent modification issues.

<!-- dth:chunk c27745d263e24c81 -->
## `Server.serve`

Handles a single SMTP client connection. Implements EHLO/HELO, AUTH PLAIN, MAIL FROM, RCPT TO (with optional rejection), DATA (with dot-unstuffing), RSET, NOOP, and QUIT commands. Requires authentication before accepting mail if credentials are configured. Stores received messages (From, To, Data, User) in a thread-safe manner.

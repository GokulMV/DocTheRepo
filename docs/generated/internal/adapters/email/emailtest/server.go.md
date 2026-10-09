<!-- dth:generated source="internal/adapters/email/emailtest/server.go" — edit only inside dth:human blocks -->
# `internal/adapters/email/emailtest/server.go`

This file provides a fake SMTP server for testing email functionality, supporting configurable authentication and message rejection.

<!-- dth:chunk 661a3731dd7bebad -->
## `Server`

A fake SMTP server for testing email functionality. The server listens on `Addr` and supports AUTH PLAIN (when configured via `SetAuth`) and rejection of RCPT TO commands (when configured via `SetReject`). It stores received messages in `mail` and protects concurrent access with `mu`.

<!-- dth:chunk 670f14e73b69d0a9 -->
## `Server.SetAuth`

Sets required AUTH PLAIN credentials for the server. When both user and pass are non-empty, clients must authenticate with these credentials. When user is empty, authentication is not required. Thread-safe.

<!-- dth:chunk 3e9e54c1fddfa304 -->
## `Server.SetReject`

Configures the server to reject RCPT TO commands with the given error text. When text is empty, RCPT TO is accepted. Thread-safe.

<!-- dth:chunk 8a80f50ccfdea6e3 -->
## `Server.conf`

Returns the current AUTH PLAIN credentials and rejection text in a thread-safe manner by holding the mutex lock.

<!-- dth:chunk e886bb6b7395f1b3 -->
## `Server.URL`

Returns the smtp:// URL needed to connect to the server, including AUTH PLAIN credentials if configured. The URL includes `?starttls=off` since the server does not support TLS.

<!-- dth:chunk c27745d263e24c81 -->
## `Server.serve`

Handles an individual SMTP client connection. It implements the SMTP protocol, enforcing authentication if configured, accepting MAIL FROM/RCPT TO/DATA commands, rejecting RCPT TO if configured, storing received messages, and supporting RSET, NOOP, and QUIT. Authentication uses AUTH PLAIN with base64 decoding. Clients must authenticate before sending mail if credentials are set.

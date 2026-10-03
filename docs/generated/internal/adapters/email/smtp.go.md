<!-- dth:generated source="internal/adapters/email/smtp.go" — edit only inside dth:human blocks -->
# `internal/adapters/email/smtp.go`

<!-- dth:chunk 64356264763d3b3b -->
## `Message`

A simple data structure representing an email message with a recipient address, subject line, and plain text body.

<!-- dth:chunk 48c9021d292934c0 -->
## `Mailer`

Manages SMTP connections and sends emails. Stores server credentials, TLS settings, and connection timeouts. `implicitTLS` indicates SMTPS (port 465); otherwise STARTTLS is negotiated when available and required unless disabled via URL parameter. `tlsConfig` enforces TLS 1.2 minimum.

<!-- dth:chunk f8c3b422296469aa -->
## `Parse`

Constructs a Mailer from an SMTP URL and From address. Accepts `smtp://` (STARTTLS, port 587 default), `smtps://` (implicit TLS, port 465 default), or untrusted relays via `?starttls=off`. Returns error if URL scheme is invalid, host is missing, or the From address is malformed. Sets a 15-second dial timeout.

<!-- dth:chunk ccc21d1b8e14ff2d -->
## `Mailer.From`

From is the sender address.

<!-- dth:chunk f297d665c838d6aa -->
## `Mailer.Server`

Server is host:port, for status pages (never the credentials).

<!-- dth:chunk 8a6c1b9efb0eec61 -->
## `Mailer.Send`

Connects to the SMTP server, authenticates if credentials exist, validates recipient and sender addresses, and transmits the composed message. Handles both implicit TLS and STARTTLS negotiation; rejects STARTTLS-less connections unless explicitly permitted. Sets connection deadline from context or defaults to one minute. Returns detailed errors at each stage (connection, auth, SMTP protocol).

<!-- dth:chunk a3aa17888d12f0de -->
## `Mailer.compose`

Formats the email message as RFC 5322 compliant bytes with standard headers (From, To, Subject, Date, Message-ID, MIME headers). Generates a random Message-ID using the sender's domain. Encodes the subject with quoted-printable encoding for non-ASCII characters. Normalizes line endings to CRLF before appending the body; the SMTP data writer handles dot-stuffing.

<!-- dth:chunk 72816a768bd09de7 -->
## `Mailer.Port`

Port returns the port as a number, for tests and status.

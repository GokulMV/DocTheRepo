<!-- dth:generated source="cmd/hub/invite.go" — edit only inside dth:human blocks -->
# `cmd/hub/invite.go`

<!-- dth:chunk ac74d7ed30ac479f -->
## `invite`

Generates a one-time password invitation link for a user, for emergency access when single sign-on is broken and password authentication is disabled. Creates a new user if the email doesn't exist, otherwise uses the existing user; accepts an optional role argument (viewer, editor, admin, owner) defaulting to owner for new users. Outputs the invitation link with its expiration time and warns if password sign-in is currently disabled. Returns error if arguments are malformed, config/database fails to load, or role creation/invite generation fails.

<!-- dth:chunk 062c6d0b63e8ed9c -->
## `inviteMain`

Command entry point for the invite subcommand; calls `invite` with CLI arguments and config path, writes errors to stderr, and returns exit code 0 on success or 1 on failure.

<!-- dth:generated source="cmd/dth/commands.go" — edit only inside dth:human blocks -->
# `cmd/dth/commands.go`

<!-- dth:chunk 447e958b3fdde69f -->
## `app`

Holds CLI state and configuration for the dth command. `out` and `errOut` are output writers for normal and error messages. `server` and `token` store the hub URL and authentication token, respectively; `profile` is the named profile to use. `asJSON` controls output formatting. The optional `runner` field overrides the default Docker Compose executor for testing purposes.

<!-- dth:chunk 793b08266e390ab6 -->
## `newRoot`

Constructs the root cobra Command for the dth CLI. Loads configuration from disk and sets up an app instance with output writers and command-line flags for server, token, profile, and JSON output. The PersistentPreRunE hook resolves the active profile (unless running login/profile commands), then falls back to environment variables and defaults for server and token. Registers all CLI subcommands (up, down, login, status, ask, etc.) to the root command.

<!-- dth:chunk a455a313de521494 -->
## `app.upCmd`

Implements the `up` command to start or upgrade the local DocTheRepo Hub via Docker Compose. Passes configuration and output settings to `bootstrap.Up()`, then displays the hub URL and setup instructions. If setup was successful, saves the profile with the returned URL and token. Optionally opens a browser to the setup page or owner setup link unless `--no-browser` is set. Supports flags for state directory, hub image, port, owner email, Ollama, and upgrade mode.

<!-- dth:chunk 6064aec7d5c335ce -->
## `app.loginCmd`

Implements the `login` command to authenticate and save hub credentials. Requires `--token` (a personal access token created in the hub UI). Calls `/me` endpoint to verify the token and retrieve the user's email and role. On success, saves the profile with server URL and token to disk, then displays the signed-in user's details. Returns an error if no token is provided.

<!-- dth:generated source="cmd/dth/commands.go" — edit only inside dth:human blocks -->
# `cmd/dth/commands.go`

Defines CLI commands and handlers for the DocTheRepo Hub CLI tool, including repository management, job handling, authentication, and server control.

<!-- dth:chunk 793b08266e390ab6 -->
## `newRoot`

Creates and configures the root Cobra command for the `dth` CLI tool. Initializes an `app` instance with provided I/O writers, loads configuration, and sets up the root command with help text, version info, and persistent flags for `--server`, `--token`, `--profile`, and `--json`. The `PersistentPreRunE` hook resolves configuration and environment variables before command execution, skipping resolution for `login` and `profile` commands. Defaults to http://localhost:8080 for server and allows overriding via environment variables (`DTH_SERVER`, `DTH_TOKEN`, `DTH_PROFILE`) or command-line flags. Finally, it attaches all subcommands (up, down, login, ask, repos, jobs, etc.) to the root command and returns it.

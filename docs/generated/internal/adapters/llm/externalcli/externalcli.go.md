<!-- dth:generated source="internal/adapters/llm/externalcli/externalcli.go" — edit only inside dth:human blocks -->
# `internal/adapters/llm/externalcli/externalcli.go`

<!-- dth:chunk b91bbb4c5dddeb98 -->
## `Engine.generateWith`

### generateWith

Executes an external CLI engine to generate documentation by marshaling the task to JSON, invoking the engine via `run()`, and parsing the result. Validates the job ID to prevent path traversal, creates a temporary work directory that is cleaned up after completion, and handles various error conditions distinctly: returns permanent errors for invalid inputs and engine-reported failures, transient errors for I/O issues, and context errors as-is. If the engine times out, wraps the error as transient; if it exits with a nonzero status, attempts to extract the error message from the result JSON (if readable) before falling back to stderr. Validates the result JSON against the schema and checks that the engine reported success status.

<!-- dth:chunk bba312baa70b7a4f -->
## `Engine.run`

### run

Spawns an external process with the provided task and result file paths, interpolating them and the model name into the configured command arguments. Sets up a deadline context using the engine's timeout duration, captures stderr up to 64 KB (and discards stdout), and sets a 5-second wait delay before force-killing the process. Returns stderr bytes and any error; if the deadline is exceeded, returns `context.DeadlineExceeded` even if the command exited successfully, allowing the caller to distinguish timeout from other errors.

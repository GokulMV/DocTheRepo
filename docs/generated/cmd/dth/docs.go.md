<!-- dth:generated source="cmd/dth/docs.go" — edit only inside dth:human blocks -->
# `cmd/dth/docs.go`

Implements CLI commands for estimating, writing, and reporting on repository documentation using the DocTheRepo API.

<!-- dth:chunk 73491c3c8f0e0ceb -->
## `app.docsCmd`

Creates and returns a Cobra command that groups three documentation subcommands: `estimate`, `write`, and `report`. This serves as the parent command for all documentation-related operations on a repository.

<!-- dth:chunk 5b1dbfa0bbdcf871 -->
## `app.docsEstimateCmd`

Estimates the cost of generating documentation for a repository without actually calling the model. Takes `owner/repo` as an argument and queries the `/repos/{id}/docs/estimate` endpoint. Returns document count (with breakdown of unchanged and modules) and token/USD estimates. The `--full` flag forces repricing of all documents instead of just missing or changed ones. Output defaults to human-readable format or JSON if `--asJSON` is set.

<!-- dth:chunk dae39ef75dfbdfcd -->
## `app.docsWriteCmd`

Generates and writes documentation for a repository against its monthly cap. Takes `owner/repo` as an argument. Supports `--full` to rewrite all documents, `--wait` to block until completion with progress updates, `--cap` to set the USD budget before writing, and `--only` to generate specific document types. Returns the job ID immediately; optionally polls the job status via `waitJob()` if `--wait` is set.

<!-- dth:chunk 6c6328e4dd1098ec -->
## `app.waitJob`

Polls a job at 3-second intervals until it reaches a terminal state, printing status updates to the provided writer whenever the status or progress changes (with timestamp). Returns nil if the job completes successfully; returns an error if the job ends in a non-Done state or if the context is cancelled. Used by commands that need to monitor long-running operations.

<!-- dth:chunk dc927a7a2e98a2e6 -->
## `app.docsReportCmd`

Generates a report on a repository's documentation quality covering cost, repairs, confidence, and weak/off-length sections. Takes `owner/repo` as an argument. The `--text` flag includes full document prose; by default only titles, scores, and check findings are included. Output format is Markdown by default or JSON if `--asJSON` is set. The `--out`/`-o` flag writes the report to a file instead of stdout.

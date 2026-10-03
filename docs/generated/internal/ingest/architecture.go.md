<!-- dth:generated source="internal/ingest/architecture.go" — edit only inside dth:human blocks -->
# `internal/ingest/architecture.go`

<!-- dth:chunk 0aa7bf4844039a9e -->
## `ScanResult`

Reports the results of a repository scan for architecture diagrams, including counts of found and removed diagrams, checked candidates, skipped files (when the limit is reached), any files that failed to read, and the commit SHA at which the scan occurred.

<!-- dth:chunk fcea46ef570f0b56 -->
## `ScanFailure`

ScanFailure is one file a scan could not read.

<!-- dth:chunk ffbf2c9f895d2cf8 -->
## `HostError`

HostError is a scan failure talking to the git host (reading the branch or the file list).

<!-- dth:chunk bd8346370f67d403 -->
## `HostError.Error`

Returns a human-readable error message by concatenating the operation description with the underlying error text (with retry-class prefixes removed for clarity).

<!-- dth:chunk 89d13a2c1dd6f387 -->
## `plainError`

Strips transient and permanent retry-class prefixes from an error's text, returning a clean message suitable for human consumption.

<!-- dth:chunk f57cc7652d2c050d -->
## `HostError.Unwrap`

Implements error unwrapping to expose the underlying error from a HostError, enabling use with errors.Is and errors.As.

<!-- dth:chunk f058f9cba9592a7e -->
## `ArchitectureSync.Scan`

Reads the tracked branch head of a repository, lists all files in its tree, identifies diagram candidates up to a limit, syncs each candidate via syncFile, collects failures without stopping, and uses KeepDiagrams to remove old diagrams while retaining those found and those that failed to read. Returns early on context cancellation. Errors from the git host operations are wrapped in HostError for better context.

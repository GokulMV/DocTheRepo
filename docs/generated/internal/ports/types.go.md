<!-- dth:generated source="internal/ports/types.go" — edit only inside dth:human blocks -->
# `internal/ports/types.go`

<!-- dth:chunk 9bf6ca331e1836ad -->
## `Job`

A queued unit of work with full lifecycle tracking. Core fields include `ID` (unique identifier), `Type` (what kind of job), `Status` (current state), and `Payload` (encoded job parameters). Lock fields (`LockedBy`, `LockedUntil`) coordinate distributed processing. `Attempts` and `MaxAttempts` control retries; `RunAfter` delays execution. `CorrelationID` groups related operations, `SerialKey` and `DedupeKey` control ordering and deduplication. `Progress` carries a running job's latest `JobProgress` report. `Result` and `Error` store outcomes; `ReplayedFrom` tracks job replay history.

<!-- dth:chunk 9d56007dce352f0f -->
## `JobProgress`

Represents a progress report from a running job, containing the current stage name (e.g., "documenting"), count of items completed versus total items in that stage, and optionally the name of the item currently being processed. Used by jobs to communicate incremental progress to observers.

<!-- dth:chunk 5e5094f0d2806d30 -->
## `__module__`

Defines the set of valid job types (JobCodePush, JobDecodeIssue, JobKnowledgeSync, JobImportDocs, JobReindex, JobSignalBatch, JobPRReview, JobSecurityScan, JobSecurityFix) and job statuses (queued through dead), with `AllJobTypes` and `AllJobStatuses` slices enumerating all possible values for validation and iteration. JobPRReview advances docs PRs post-review; JobSecurityScan and JobSecurityFix handle security operations.

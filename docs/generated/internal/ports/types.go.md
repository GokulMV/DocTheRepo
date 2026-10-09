<!-- dth:generated source="internal/ports/types.go" — edit only inside dth:human blocks -->
# `internal/ports/types.go`

Defines port types and enums for job management, including job types, statuses, and related data structures.

<!-- dth:chunk 5e5094f0d2806d30 -->
## `__module__`

Defines job types and statuses used throughout the system. `JobType` constants categorize work items: `JobCodePush`, `JobRepoDocs`, `JobSystemDocs`, and `JobPRReview` handle documentation generation and updates; `JobSecurityScan` and `JobSecurityFix` manage security operations; others handle knowledge management and indexing. `JobStatus` constants track job lifecycle: `JobQueued` through `JobProcessing` to terminal states (`JobDone`, `JobFailed`, `JobAborted`, `JobDead`), with intermediate states `JobSpendBlocked`, `JobPendingApproval`, and `JobNeedsHuman` for blocking conditions. `AllJobTypes` and `AllJobStatuses` slice exports enumerate all valid values for iteration and validation.

<!-- dth:generated source="internal/core/security/jobs.go" — edit only inside dth:human blocks -->
# `internal/core/security/jobs.go`

<!-- dth:chunk 198ad15861896364 -->
## `ScanPayload`

Payload structure for initiating a security scan job, containing the scan ID, target repository ID, and optional list of modules to scan.

<!-- dth:chunk 42160afaeb526484 -->
## `FixPayload`

FixPayload fixes selected findings of one repository in one pull request.

<!-- dth:chunk 0d6207fdd4ad3b11 -->
## `Store`

Interface for persisting security scan state and findings. Manages scan lifecycle (start/finish), stores discovered findings, retrieves findings by ID, and updates fix status and pull request URLs for individual findings.

<!-- dth:chunk 1cf1038da8f45167 -->
## `Jobs`

Orchestrator for security scan and fix jobs. Coordinates scanning via Scanner, persists results via Store, retrieves repository configuration and code host integrations to determine scan targets.

<!-- dth:chunk 08b146b39b8728be -->
## `Jobs.target`

Resolves a repository ID to its scan target by fetching repo configuration, code host, and current branch head commit. Returns error if repository not found, code host unavailable, or branch head cannot be read.

<!-- dth:chunk d5b0d74ad7e8cb48 -->
## `Jobs.HandleScan`

Executes a security scan job: unmarshals payload, retrieves target repository, runs scanner, stores findings, and records completion status. Distinguishes spend-blocked errors (returns JobSpendBlocked) from fatal errors (returns Permanent). Returns detailed summary including finding counts, files scanned, and tokens used.

<!-- dth:chunk b83dc261bd5e9963 -->
## `Jobs.HandleFix`

Executes a fix job for selected findings: retrieves findings from store, proposes fixes via scanner (applying each fix to context for subsequent fixes), handles spend-blocked errors, opens a pull request with all proposed changes, and updates finding status to "pr_opened". Returns early with JobDone if no fixes could be proposed; errors during fix proposal are recorded per-finding and continue processing remaining findings.

<!-- dth:chunk e47b73da7c476435 -->
## `Jobs.openPR`

Creates a pull request with proposed fixes: creates a branch named `dth/security-fix-` + truncated job ID, commits all changed files with bot identity, and opens PR with detailed body documenting each fixed finding, tests, and risk assessment. Returns PullRequest with URL or error if branch creation or commit fails.

<!-- dth:chunk 17dab396860ee07c -->
## `prList`

Formats findings as a simple bullet-list string for inclusion in pull request commit messages, listing priority, dimension, title, file, and line number for each finding.

<!-- dth:chunk 2018d6f68285fe19 -->
## `plural`

Returns empty string for singular, "s" for plural counts; used in pull request title generation.

<!-- dth:chunk 320aeb4e93dfd275 -->
## `__module__`

Job type constants mapping to ports package equivalents for security scan and fix job dispatching.

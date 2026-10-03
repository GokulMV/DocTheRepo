<!-- dth:generated source="internal/store/security.go" — edit only inside dth:human blocks -->
# `internal/store/security.go`

<!-- dth:chunk 49f4d14a4c783e1a -->
## `Security`

Security stores security scans and findings.

<!-- dth:chunk d590f06582dd97d5 -->
## `NewSecurity`

NewSecurity returns the security store.

<!-- dth:chunk 65a60012f35f0374 -->
## `Scan`

Scan represents a security scan result with metadata and findings. The Findings field is populated when a Scan is retrieved individually (e.g., via GetScan), but omitted when returned in lists.

<!-- dth:chunk 1972e647f593f429 -->
## `Security.CreateScan`

CreateScan records a new queued scan in the database and returns its ID. It stores the repository ID, modules to scan, and the user who initiated the scan. Returns an error if the database insert fails.

<!-- dth:chunk d3a099ac24741f9d -->
## `Security.SetScanJob`

SetScanJob links the scan to its job.

<!-- dth:chunk d054f5c37c2cecd0 -->
## `Security.StartScan`

StartScan implements security.Store.

<!-- dth:chunk bfa5f83f263d9be9 -->
## `Security.FinishScan`

FinishScan updates a scan with its completion status, verdict, summary JSON, and optional error message. Sets finished_at to the current time. Serializes the summary to JSON as `{}` if nil.

<!-- dth:chunk e11b67daeabc7e03 -->
## `Security.AddFindings`

AddFindings inserts finding records for a scan into the database within a transaction. Filters out nil values from Repro before serializing to JSON. Returns the first error encountered if insertion fails for any finding.

<!-- dth:chunk c5b989383f113b18 -->
## `scanFinding`

scanFinding unmarshals a database row into a Finding struct, parsing JSON-encoded Repro and Fix fields. Ignores Fix if its JSON is 2 bytes or smaller (empty object). Returns any scanning error.

<!-- dth:chunk 43a441fafb228bc1 -->
## `Security.findings`

findings queries findings from the database with a custom WHERE clause and returns them ordered by priority and status (confirmed > plausible > other), then by file and line number. Returns any query or row scanning error.

<!-- dth:chunk d04bbbfb9324f687 -->
## `Security.Findings`

Findings implements security.Store: the given findings of one repository.

<!-- dth:chunk 9955d3e8f7fc454a -->
## `Security.SetFix`

SetFix updates a finding's fix metadata: status, fix details JSON, pull request URL, and error message. Only updates fix_pr_url if the provided URL is non-empty; preserves the existing fix object if the new one is empty.

<!-- dth:chunk 5d1178b455457c73 -->
## `Security.QueueFixes`

QueueFixes marks findings as queued for fixing, only updating those that are verified (not rejected) and not already queued or in a pull request. Returns the IDs of findings actually updated.

<!-- dth:chunk a7c7f22c368926e4 -->
## `Security.ModuleFindings`

ModuleFindings retrieves cached findings for a specific module identified by repository ID, module name, and input hash. Returns the findings list, a boolean indicating success, and any error.

<!-- dth:chunk fe23479124739c7c -->
## `Security.PutModuleFindings`

PutModuleFindings inserts or updates cached findings for a module. Treats nil findings as an empty slice and serializes to JSON for storage. Updates the cached_at timestamp on conflict.

<!-- dth:chunk 1584fa5432eb789e -->
## `scanScan`

scanScan unmarshals a database row into a Scan struct without its findings. Returns any scanning error.

<!-- dth:chunk a0672d714da65bb1 -->
## `Security.GetScan`

GetScan retrieves a single scan by ID with all its findings populated. Returns ErrNotFound if the scan does not exist; populates findings by querying the findings table separately.

<!-- dth:chunk 66751d3e8d4d9a23 -->
## `Security.LatestScans`

LatestScans returns the most recent scan for each repository, optionally restricted to specific repositories. When `all` is true, includes all repositories; otherwise filters to the provided repoIDs.

<!-- dth:chunk d030c1de907ef83c -->
## `Security.RepoScans`

RepoScans lists a repository's scans ordered newest first, limited by the provided count. Returns any query error.

<!-- dth:chunk e3d6221f57c561bf -->
## `nonNilStrings`

nonNilStrings returns the input slice unchanged if non-nil, otherwise returns an empty slice. Used to prevent passing nil to SQL array parameters.

<!-- dth:chunk d667e8cea0f50a65 -->
## `__module__`

SQL column lists for selecting findings and scans with their associated repository and user data. findingCols includes all finding fields and their fix metadata; scanCols joins to repos and users tables, using empty string defaults for missing job_id and email.

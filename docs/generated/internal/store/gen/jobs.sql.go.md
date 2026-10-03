<!-- dth:generated source="internal/store/gen/jobs.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/jobs.sql.go`

<!-- dth:chunk c41c00ccb7e32900 -->
## `Queries.ClaimJob`

Atomically claims the next runnable job of a given type for processing. Jobs with a serial key only become runnable when no earlier job with the same key is queued or processing, enforcing serialization per key while allowing parallel execution across keys. Uses `SKIP LOCKED` to ensure exactly-once claiming in concurrent environments. Returns the claimed job with all fields including the new progress field.

<!-- dth:chunk c0dd199471213e96 -->
## `Queries.EnqueueJob`

Inserts a new job into the queue, returning its full state including the generated sequence number and progress field. Uses `ON CONFLICT` to silently skip insertion if a job with the same dedupe_key already exists in queued or processing status, providing deduplication on insert.

<!-- dth:chunk aeae5b8de3b1d5ba -->
## `Queries.GetJob`

Retrieves a job by ID, returning all job fields including the progress field. Used for inspecting job state and history.

<!-- dth:chunk 01e65a8b4120d221 -->
## `Queries.GetLiveJobByDedupeKey`

Retrieves a job by dedupe key if one exists in queued or processing status, returning all job fields including progress. Returns the first live job matching the key; used to check for duplicate or ongoing work.

<!-- dth:chunk 2065a11eca392a72 -->
## `Queries.ListJobs`

Lists jobs matching optional filters (status, type, repo_id) ordered by descending sequence number, limited to maxRows. Returns an empty slice if no rows match; rows are scanned into Job structs including the progress field.

<!-- dth:chunk f40688ded26b3c39 -->
## `SetJobProgressParams`

Parameters for updating a job's progress field. `Progress` holds a JSON-encoded progress state, and `ID` identifies the job to update.

<!-- dth:chunk 0d7057e34cf9c509 -->
## `Queries.SetJobProgress`

Updates the progress field for a processing job, returning the number of rows affected. Executes against the `setJobProgress` query which updates only if the job status is 'processing'.

<!-- dth:chunk 5016e75219bcded5 -->
## `__module__`

SQL query constants for job operations. Added `setJobProgress` to update progress during processing. Updated `claimJob`, `enqueueJob`, `getJob`, `getLiveJobByDedupeKey`, and `listJobs` to include the new `progress` column in their RETURNING or SELECT clauses. Other queries (`finishJob`, `heartbeatJob`, `notifyJobs`, `queueDepth`, `reclaimExpiredLeases`, `rescheduleJob`) remain unchanged.

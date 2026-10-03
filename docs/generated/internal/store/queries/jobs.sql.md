<!-- dth:generated source="internal/store/queries/jobs.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/jobs.sql`

<!-- dth:chunk 85ebf31fd98fb7ac -->
## `internal/store/queries/jobs.sql`

This file defines SQL queries for a job queue system using sqlc. It includes operations for enqueueing jobs with deduplication, claiming jobs for processing with serial key constraints (ensuring sequential execution within keys), updating job state during execution (heartbeat, completion, rescheduling), reclaiming leases from timed-out workers, and querying job status. The ClaimJob query uses SKIP LOCKED for concurrent-safe claiming and prioritizes by priority/sequence. The FinishJob and RescheduleJob queries are fenced by locked_by to prevent workers with expired leases from corrupting state. The ReclaimExpiredLeases query transitions stuck jobs to dead/queued based on max_attempts. Supporting queries enable job lookup, filtering/listing, monitoring queue depth, and progress tracking.

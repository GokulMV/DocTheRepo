<!-- dth:generated source="internal/store/queries/analytics.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/analytics.sql`

<!-- dth:chunk fa7fd980df0f6dd8 -->
## `internal/store/queries/analytics.sql`

This file defines nine analytics queries for generating usage statistics, operational metrics, and activity logs from the application database.

**UsageSeries** aggregates API call metrics by time granularity (hour/day) and a grouping dimension (feature, provider, model, repo, user, or all combined), returning call counts, token usage, costs, cache hits, and blocked calls.

**SavingsByKind** sums cost and token savings from cache operations grouped by savings event type.

**PipelineStats** reports job counts and latency percentiles (p50, p95) grouped by job type and status within a time range.

**RepoFreshness** lists enabled repositories with their last processed commit SHA and most recent completed job timestamp for freshness tracking.

**ConnectorStats** shows connector metadata (type, health status, last sync) and daily job counts for each connector.

**ActivityJobs** returns recently completed/failed job records filtered by optional repository scope, ordered by recency.

**ActivityPRs** fetches recently updated pull request records filtered by optional repository scope.

**ActivityAudit** returns recent audit log entries documenting user actions and system changes.

All queries use sqlc parameter binding for SQL injection prevention and type safety.

<!-- dth:generated source="internal/store/queries/analytics.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/analytics.sql`

SQL analytics queries for generating usage reports, pipeline metrics, connector health status, audit activity logs, and source picker performance summaries.

<!-- dth:chunk fa7fd980df0f6dd8 -->
## `internal/store/queries/analytics.sql`

Defines analytics and reporting queries for usage tracking, pipeline metrics, connector health, audit activity, and source picker performance.

**UsageSeries** returns time-bucketed call and token usage grouped by feature, provider, model, repository, user, or aggregated ('all'), with filtering by date range and optional user ID.

**SavingsByKind** aggregates cache-related cost and token savings by event kind over a time period.

**PipelineStats** computes job counts and latency percentiles (p50, p95 seconds) by job type and status within a time window.

**RepoFreshness** lists enabled repositories with their last processed SHA and the most recent successful 'code_push' job timestamp.

**ConnectorStats** reports connector health, last sync time, last error, and count of jobs created in the past day.

**ActivityJobs**, **ActivityPRs**, and **ActivityAudit** return recent completed/failed jobs, pull request updates, and audit log entries respectively, with optional repository filtering and result limits.

**SiftTotals** and **SiftDaily** summarize Ask's source picker results from the `sift` JSON field in QA messages, including candidate filtering, token savings, and costs.

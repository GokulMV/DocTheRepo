<!-- dth:generated source="internal/store/gen/analytics.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/analytics.sql.go`

<!-- dth:chunk ed408b10eb981ada -->
## `UsageSeriesRow`

Represents a single row of usage analytics data aggregated by time period and grouping dimension. Fields track API call metrics including token usage, costs, caching statistics, and blocked requests for monitoring and billing purposes.

<!-- dth:chunk 800c676f1a8e1fc0 -->
## `Queries.UsageSeries`

Queries the usage events table bucketed by specified time granularity (hour or day) and grouped by a single dimension (feature, provider, model, repo, or user). Executes a parameterized query with granularity, grouping key, time range, and optional user filter, returning aggregated metrics or an error if the query or row scanning fails.

<!-- dth:chunk a3cb334d8d5c231c -->
## `__module__`

Contains SQL query string constants for analytics operations including activity audits, job/PR tracking, connector and pipeline statistics, repository freshness, savings events, and usage series aggregation. The `usageSeries` query aggregates usage events by configurable time granularity and dimension, computing counts, token sums, costs, cache metrics, and blocked request counts.

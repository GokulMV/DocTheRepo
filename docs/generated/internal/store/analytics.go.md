<!-- dth:generated source="internal/store/analytics.go" — edit only inside dth:human blocks -->
# `internal/store/analytics.go`

<!-- dth:chunk 8a1bf3a0ad33b479 -->
## `UsagePoint`

One aggregated data point in a usage series, representing metrics for a time bucket. Contains request counts, token usage (including cached tokens billed at a lower rate), cost in USD, and blocked request counts.

<!-- dth:chunk f18ed52b1f7cf6ae -->
## `Browse.Usage`

Aggregates usage events from the database into a time-series report, grouped by the specified dimension (feature, provider, model, repo, user, or empty for overall). Validates that `granularity` is "hour" or "day", queries the database with the provided time range and optional userID filter, and returns a UsageReport with series organized by group key and running totals across all data points.

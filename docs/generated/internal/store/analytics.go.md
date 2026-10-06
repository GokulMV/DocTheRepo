<!-- dth:generated source="internal/store/analytics.go" — edit only inside dth:human blocks -->
# `internal/store/analytics.go`

Package store provides database access for analytics and browsing functionality in DocTheRepo.

<!-- dth:chunk 505c5123b639111d -->
## `SiftReport`

Aggregate metrics for Ask's source picker performance over a period. Tracks how many sources were retrieved, how many were selected for the answering model, and cost/token savings from filtering. The `Daily` field breaks down metrics by calendar day. Fields like `Candidates` and `Kept` measure source filtering; `TokensSaved` and `SavedUSD` quantify the benefit. `JudgeTokens` and `JudgeCost` track the cost of the picker itself, allowing net savings calculation via `SavedUSD`.

<!-- dth:chunk d166b95495d65534 -->
## `SiftDay`

One day's metrics from the source picker, bucketed by calendar date. Records how many answers were processed, sources retained, tokens saved, and net USD savings for that day.

<!-- dth:chunk 93bb3b2b66b14b88 -->
## `Browse.Sift`

Queries totals and daily breakdowns of source picker metrics for a time range, aggregating data from Ask answer summaries. Calls `SiftTotals` to fetch overall statistics and `SiftDaily` to populate the per-day breakdown, then constructs a `SiftReport` from the results. Returns an error if either query fails.

<!-- dth:chunk ebdbf3445fbae1d9 -->
## `Browse.HasIssues`

Determines whether to display the Issues pages based on the presence of connected alert sources or existing issues. Returns `true` if either an active connector (excluding signal-type connectors like GitHub, GitLab, Confluence, Jira, Notion, and upload sources) exists or at least one issue record is present in the database. Returns `true` on query errors as a safe default to show the feature.

<!-- dth:chunk 8da40f68f1347424 -->
## `__module__`

Defines the set of connector types that represent signal sources (external integrations like version control, project management, and documentation platforms) and should be excluded when determining if alert-capable connectors are configured.

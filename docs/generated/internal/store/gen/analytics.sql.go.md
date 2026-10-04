<!-- dth:generated source="internal/store/gen/analytics.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/analytics.sql.go`

Generated SQL query methods for analytics, providing query execution and result scanning for source picker activity, pipeline statistics, connector health, usage tracking, and other system metrics.

<!-- dth:chunk 923182c73cf5e6fd -->
## `SiftDailyParams`

Input parameters for `SiftDaily` query, specifying the time range to query source picker activity. `FromT` is the inclusive start time and `ToT` is the exclusive end time.

<!-- dth:chunk a4d8efc50d165e1a -->
## `SiftDailyRow`

Row result from `SiftDaily` query representing aggregated source picker activity for a single day. Fields include the day, count of answers picked, total tokens saved, and USD value of savings.

<!-- dth:chunk e6a864549e8d1732 -->
## `Queries.SiftDaily`

Queries the database for daily source picker activity aggregated by day within a time range. Returns a slice of `SiftDailyRow` with day-level statistics on picks, tokens saved, and monetary savings. Scans from the `qa_messages` table filtering for assistant messages with sift data.

<!-- dth:chunk bf214917ff757e66 -->
## `SiftTotalsParams`

Input parameters for `SiftTotals` query, specifying the time range to aggregate source picker summary statistics. `FromT` is the inclusive start time and `ToT` is the exclusive end time.

<!-- dth:chunk d2f94038509201b7 -->
## `SiftTotalsRow`

Single row result from `SiftTotals` query representing aggregated source picker statistics over a time period. Includes counts of answers, picks, trimmed sources, candidates evaluated, and kept sources, plus token and cost metrics for both the judge process and token savings achieved.

<!-- dth:chunk f9d169c49c668880 -->
## `Queries.SiftTotals`

Returns aggregate statistics about Ask's source picker performance over a time period, computed from sift metadata stored with each QA message. Single result summarizes total answers processed, sources picked and trimmed, evaluation metrics, and monetary impact. Returns only the single row or scan error.

<!-- dth:chunk a3cb334d8d5c231c -->
## `__module__`

SQL query constants for analytics queries, including new `siftDaily` and `siftTotals` queries that compute source picker metrics from the `qa_messages` table by extracting and aggregating JSON sift metadata fields. Also defines constants for activity, pipeline, connector, and usage analytics queries.

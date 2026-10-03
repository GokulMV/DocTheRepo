<!-- dth:generated source="internal/ports/spend.go" — edit only inside dth:human blocks -->
# `internal/ports/spend.go`

<!-- dth:chunk e603d7f5334dbd7f -->
## `UsageRecord`

Represents a single tracked API call with resource usage and cost information, written to the ledger after completion. Includes token usage (with separate counts for cache operations), monetary cost, latency, and contextual identifiers. The `Cached` field indicates whether the output came from provider cache, while `Estimated` flags cases where actual usage was unavailable and an estimate was recorded instead. The `Outcome` field is one of "ok", "error", or "blocked".

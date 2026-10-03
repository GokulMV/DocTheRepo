<!-- dth:generated source="internal/core/spendguard/spendguard.go" — edit only inside dth:human blocks -->
# `internal/core/spendguard/spendguard.go`

<!-- dth:chunk bf31d8a707d39a36 -->
## `Price`

Per-million-token pricing structure for a specific provider and model combination. InputPerMTok, OutputPerMTok, and EmbedPerMTok define base rates; CacheReadPerMTok and CacheWritePerMTok optionally price prompt-cache tokens separately (nil means fall back to InputPerMTok). Maintained by operators in a cost table.

<!-- dth:chunk 89464b48a330d22b -->
## `Guard.CostUsage`

Calculates the cost of actual token usage, properly accounting for prompt-cache tokens which have separate pricing. Takes total input tokens (including cache), output tokens, and separate counts for cache-read and cache-write tokens. If the model has no cache pricing configured, treats cache tokens as plain input at the standard rate. Returns cost in USD and a boolean indicating if pricing was found; clamps negative plain token counts to zero to handle edge cases where reported cache tokens exceed input tokens.

<!-- dth:chunk c62a62548bd56ccd -->
## `Enforcer.Record`

Records actual token usage to the ledger after an API call completes. Defaults the timestamp to now if not set, auto-calculates cost via CostUsage if not provided, and defaults outcome to "ok". Returns an error if ledger recording fails, wrapped with context.

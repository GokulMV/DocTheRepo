<!-- dth:generated source="internal/core/spendguard/spendguard.go" — edit only inside dth:human blocks -->
# `internal/core/spendguard/spendguard.go`

SpendGuard manages spend limits and pricing calculation for API calls across different providers and models.

<!-- dth:chunk bf31d8a707d39a36 -->
## `Price`

Per-million-token pricing for a specific provider and model. Includes standard input and output rates, embedding rate, and optional cache-specific rates (defaulting to input rate if not set). The long-prompt threshold enables tiered pricing: prompts exceeding this token count apply alternate rates from the `Long` struct for all tokens in the request.

<!-- dth:chunk 60462de087e098c4 -->
## `Price.forPrompt`

Returns the prices applicable to a prompt of a given token count. If a long-prompt threshold is configured and the prompt exceeds it, returns the alternate pricing tier; otherwise returns the original pricing. This enables tiered pricing where longer prompts trigger different per-token rates.

<!-- dth:chunk 5b8564b6df99d986 -->
## `Guard.Cost`

Calculates the cost of an API call given the prompt and output token counts. Returns false if no pricing exists for the provider/model combination. For embeddings, uses the embedding rate; otherwise prices input and output separately, applying long-prompt tier pricing when the full prompt exceeds the configured threshold.

<!-- dth:chunk 89464b48a330d22b -->
## `Guard.CostUsage`

Calculates the actual usage cost when prompt caching is involved, pricing cache reads and writes at their specific rates (or as plain input if those rates aren't set). Falls back to `Guard.Cost` for embeddings or when no caching occurred. The `in` parameter represents total input tokens and determines long-prompt threshold application; plain input tokens are calculated by subtracting cache tokens (clamped to zero if caching exceeds input).

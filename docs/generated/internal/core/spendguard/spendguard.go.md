<!-- dth:generated source="internal/core/spendguard/spendguard.go" — edit only inside dth:human blocks -->
# `internal/core/spendguard/spendguard.go`

Implements spend limit enforcement with per-ceiling evaluation, in-flight call reservations, and optional budget caps to control API spending.

<!-- dth:chunk bf31d8a707d39a36 -->
## `Price`

Per-million-token pricing for a specific provider and model. Includes standard input and output rates, embedding rate, and optional cache-specific rates (defaulting to input rate if not set). The long-prompt threshold enables tiered pricing: prompts exceeding this token count apply alternate rates from the `Long` struct for all tokens in the request.

<!-- dth:chunk 60462de087e098c4 -->
## `Price.forPrompt`

Returns the prices applicable to a prompt of a given token count. If a long-prompt threshold is configured and the prompt exceeds it, returns the alternate pricing tier; otherwise returns the original pricing. This enables tiered pricing where longer prompts trigger different per-token rates.

<!-- dth:chunk af97f421adbc7696 -->
## `Request`

Describes a paid call for spend limit evaluation. Includes feature, provider, model, and repository identifiers; estimated input and output token counts; and optional operator override and per-call budget. All fields are required except Override and Budget.

<!-- dth:chunk 5cfca647ab1903c2 -->
## `Budget`

A dollar cap on top of configured spend limits, tracked separately per key (e.g., by repository and billing period). Calls with the same Budget.Key share reservations, allowing batches to stay within a single pool. The `Spent` callback retrieves current spending against the cap.

<!-- dth:chunk 1d6f9998ea8c32f2 -->
## `BufferUSD`

Computes the safety buffer to hold free under a dollar ceiling. Returns `pct` percent of `capUSD` (minimum $0.01 when `pct > 0`), capped at half the ceiling. A zero or negative `capUSD` or `pct` returns 0. The buffer absorbs estimation errors from token count variation, prompt caching, and concurrent replicas.

<!-- dth:chunk 5b8564b6df99d986 -->
## `Guard.Cost`

Calculates the cost of an API call given the prompt and output token counts. Returns false if no pricing exists for the provider/model combination. For embeddings, uses the embedding rate; otherwise prices input and output separately, applying long-prompt tier pricing when the full prompt exceeds the configured threshold.

<!-- dth:chunk 89464b48a330d22b -->
## `Guard.CostUsage`

Calculates the actual usage cost when prompt caching is involved, pricing cache reads and writes at their specific rates (or as plain input if those rates aren't set). Falls back to `Guard.Cost` for embeddings or when no caching occurred. The `in` parameter represents total input tokens and determines long-prompt threshold application; plain input tokens are calculated by subtracting cache tokens (clamped to zero if caching exceeds input).

<!-- dth:chunk 6a2e10ab45a6a473 -->
## `Limit.counts`

Determines whether a limit applies to a request by checking its scope: global limits always apply; feature, provider, and repo scopes match when their ScopeKey equals the request's corresponding field.

<!-- dth:chunk 44e7b0ae5ee472e7 -->
## `Guard.Applicable`

Returns all limits from the guard that apply to the request based on their scopes.

<!-- dth:chunk c1655ac9c5347a4e -->
## `Guard.Evaluate`

Evaluate decides a request given what each applicable limit has already spent (keyed by limit ID), keeping the default buffer under dollar ceilings. Pure: no I/O, no clock.

<!-- dth:chunk f65836e5aa7bbe67 -->
## `Guard.evaluate`

Evaluates whether a request breaches any applicable ceiling. For token limits, checks against exact totals (no buffer); for cost limits, reserves `BufferUSD(pct)` free. Returns a Decision with `Allow=false` and details of the first breached limit, or allows with utilization metrics per limit. Operator overrides are logged as warnings. When pricing is unavailable, a warning is added but evaluation continues.

<!-- dth:chunk 8915768102ccddfd -->
## `reservedTokens`

Helper that formats a string suffix for reserved tokens held by in-flight calls. Returns empty string if `n` is 0, otherwise `" + N reserved by calls in flight"`.

<!-- dth:chunk 95712d3c895fa2ac -->
## `costBreach`

Formats a detailed cost breach message showing spent, reserved (if any), estimated, and safety buffer amounts. Used when a request would exceed a cost ceiling.

<!-- dth:chunk 8b88f719881bbfc3 -->
## `Enforcer`

Enforces spend limits against a ledger, paired with Reserve and Record. Maintains the guard (limits and prices), buffer percentage, and in-flight reservations. Thread-safe: `checkMu` serializes ledger reads and decisions; `resMu` guards the reservation map alone to allow non-blocking release. Optional `OnDecision` callback observes all verdicts for logging or metrics.

<!-- dth:chunk 4700a2eeb471b303 -->
## `reservation`

Internal struct tracking a reserved call: its request, estimated worst-case token count, and estimated cost in dollars.

<!-- dth:chunk 587f2b3b247a40c6 -->
## `NewEnforcer`

NewEnforcer wires a guard to the ledger with the default buffer. alerter may be nil.

<!-- dth:chunk 2fcce886ab3e68b5 -->
## `Enforcer.SetBufferPct`

Sets the safety buffer percentage (0–50) applied to all dollar ceilings. Returns an error if `pct` is outside range or NaN. Thread-safe via mutex.

<!-- dth:chunk f168a6f0d90839c7 -->
## `Enforcer.BufferPct`

Returns the current safety buffer percentage. Thread-safe read.

<!-- dth:chunk 6f3fc5f9510311b9 -->
## `Enforcer.Check`

Evaluates a request without holding a reservation, returning whether it would be allowed or blocked. Implemented as Reserve followed by immediate release. Returns `*ports.SpendBlockedError` when blocked; ledger errors are returned as transient, never silently allowed.

<!-- dth:chunk cc1fecdb415111c5 -->
## `Enforcer.Reserve`

Atomically evaluates a request and, if allowed, reserves its estimated tokens and cost against all applicable ceilings until the returned release function is called. Reads current spend from the ledger and in-flight reservations, ensuring two calls never both consume the last of a ceiling. The release function is idempotent and safe to call more than once (typically deferred). Returns a Decision and error; errors include ledger failures (transient) and budget checks. Blocks on breached limits are returned as `*ports.SpendBlockedError`; alerts are sent best-effort if the limit has Alert=true.

<!-- dth:chunk 0d050687cd85da0f -->
## `Enforcer.checkBudget`

Applies the optional per-call Budget cap from the request, if present and enabled. Checks whether spending plus in-flight reservations plus estimate would exceed the budget's ceiling minus buffer. Returns a transient error on ledger read failure; blocks are reported in the Decision. Skips evaluation with a warning if pricing is unavailable.

<!-- dth:chunk cfae17ce0d3f7a2e -->
## `Enforcer.inFlight`

Returns a snapshot of all current reservations held by in-flight calls. Thread-safe via mutex.

<!-- dth:chunk 8da527d66e8a7ae7 -->
## `Enforcer.Reserved`

Reports the total tokens and dollars currently reserved across all in-flight calls.

<!-- dth:chunk 674d50903fe74062 -->
## `__module__`

Scope and Window constants (ScopeGlobal, ScopeFeature, ScopeProvider, ScopeRepo, WindowDay, WindowMonth), default buffer percentage (5%), and an error requiring explicit acknowledgment for unlimited ceilings.

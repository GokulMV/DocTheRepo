<!-- dth:generated source="internal/core/llmgateway/gateway.go" — edit only inside dth:human blocks -->
# `internal/core/llmgateway/gateway.go`

LLMGateway enforces spend guard limits and records usage for all external LLM, embedding, and documentation generation calls.

<!-- dth:chunk 71f905cb2ee24e01 -->
## `CallMeta`

Attributes a single LLM call for ledger recording and spend limit enforcement. Contains repository, user, job, and issue IDs; Override flags operator retries of spend-blocked calls; and optional Budget provides an additional per-call cap for a repository's documentation generation budget.

<!-- dth:chunk 1573fd5eafad0501 -->
## `Gateway.chatOn`

Executes a chat request through the LLM provider on the specified route with metadata for tracking. Protects message content, applies route defaults for temperature and effort, estimates input tokens, reserves budget for worst-case (max) output before calling the LLM, records actual usage (or estimates it if unreported), and handles billable errors separately from fatal ones. If the ledger write fails after a successful call, it surfaces the error to observability instead of returning it to avoid re-execution and re-billing.

<!-- dth:chunk 60ada6617b97374e -->
## `Gateway.Embed`

Embeds a list of texts by batching according to the embedder's MaxBatch limit (defaulting to 64), applying content protection to each batch, and calling embedBatch. Returns all resulting vectors in order, the route used, and any error from batching or embedding.

<!-- dth:chunk dbd57c8ace9a5c3a -->
## `Gateway.embedBatch`

Embeds one batch of strings within spend guard limits, recording metrics and usage. It reserves budget before calling the embedder, returns early if the reservation fails, estimates tokens if the provider doesn't report them, validates that the returned vector count matches the input count, and records latency and usage outcomes for observability.

<!-- dth:chunk 1b3116e5cee1d5a8 -->
## `Gateway.GenerateDocs`

Generates documentation via an external engine on the docgen route with spend guarding and one automatic repair pass for schema errors. Validates that the engine reports usage (or allowUnreported is true), sets per-chunk output token limits (defaulting to 2000), estimates and reserves budget, and records usage (estimated when unreported). On schema errors it retries once with repair hints; permanent schema errors after retry are returned. If the ledger write fails after generation succeeds, the error is surfaced to observability instead.

<!-- dth:generated source="internal/core/llmgateway/gateway.go" — edit only inside dth:human blocks -->
# `internal/core/llmgateway/gateway.go`

<!-- dth:chunk b3506056713aa1b0 -->
## `Gateway.record`

Records a single LLM call to the spend ledger via the enforcer. Aggregates token counts by summing input and output tokens (including cache-related tokens), formats latency in milliseconds, and captures metadata (repository, user, job, issue) and outcome status. The `estimated` flag indicates whether actual usage was reported by the provider or inferred.

<!-- dth:chunk 1b3116e5cee1d5a8 -->
## `Gateway.GenerateDocs`

Runs documentation generation through the docgen route, applying spend guards and automatic repair. Routes the request, validates the provider reports usage (unless `allowUnreported` is set), sets per-chunk output token limits (default 2000), estimates token costs, and checks spend authorization. On execution, records actual usage from the provider or falls back to estimates if unreported. Handles `SchemaError` with one automatic repair pass by feeding back problems to retry. Returns the generation result or error.

<!-- dth:chunk 5cfda32a44907fd5 -->
## `__module__`

Defines feature route identifiers for the gateway's spend-guarded LLM calls: `FeatureDocGen` (primary documentation), `FeatureDocGenFast` (optional faster variant), `FeatureQA`, `FeatureDecode`, `FeatureTriage`, `FeatureEmbedding`, `FeatureSuggest`, and `FeatureSecurity` (attack/verify/fix scans). Also declares `ErrNoRoute` (no provider configured) and initializes a default PII redactor with built-in patterns.

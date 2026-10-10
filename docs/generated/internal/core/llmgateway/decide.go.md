<!-- dth:generated source="internal/core/llmgateway/decide.go" — edit only inside dth:human blocks -->
# `internal/core/llmgateway/decide.go`

Implements LLM decision routing with native and fallback decider paths, integrating token budget enforcement and observability.

<!-- dth:chunk 5a6af8519e4239fa -->
## `Gateway.decideNative`

Executes a decision-making request through a native LLM decider with token budget enforcement and observability. It protects sensitive context, estimates token consumption (input tokens from context plus 8 per option, output tokens as 4 per option), reserves budget via the enforcer, calls the decider, and handles both reported and estimated token usage. Records latency and usage metrics, returning either the decider's result or an error if budget enforcement or the decision call fails. If the decider omits a model name, it falls back to the route's model before finalizing the decision.

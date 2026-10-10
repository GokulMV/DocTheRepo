<!-- dth:generated source="internal/core/llmgateway/judge.go" — edit only inside dth:human blocks -->
# `internal/core/llmgateway/judge.go`

This file implements token budget enforcement and observability for LLM gateway judgment operations.

<!-- dth:chunk 75d74261972dcf1b -->
## `Gateway.judgeNative`

Executes a judgment request against a native judger with token budget enforcement and observability. It estimates input tokens from the state and question instructions, and output tokens based on question count, then reserves tokens via the enforcer before invoking the judger. If reservation fails, it records the block and returns an error. After execution, it reconciles the actual token usage (or uses the estimate if unreported), records metrics including latency and outcome, and applies clamping constraints to the judgment result. Returns the judgment on success or an error if reservation or judging fails.

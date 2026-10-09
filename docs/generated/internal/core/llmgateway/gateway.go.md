<!-- dth:generated source="internal/core/llmgateway/gateway.go" — edit only inside dth:human blocks -->
# `internal/core/llmgateway/gateway.go`

Provides a gateway that enforces spend limits around LLM provider calls and handles JSON schema validation with automatic repair.

<!-- dth:chunk dc52938ea8f848c6 -->
## `JSONResult`

Holds results from a ChatJSON call: accumulated token usage across all attempts (including retries and repair calls), the model that responded, the last raw response text, and a list of validation problems that triggered a repair attempt (empty if the first response was valid).

<!-- dth:chunk 2f397337f0f609fd -->
## `Gateway.ChatJSONResult`

Calls Chat to generate JSON matching the provided schema, automatically handling truncation errors and validation failures. If the initial response is truncated, retries with double the output token budget (capped at 64000). If validation fails (checked via decode and the Check function), appends the validation problems to the conversation and makes a repair call asking the model to fix the JSON. Returns a JSONResult with cumulative usage and metadata, plus a permanent error if the repair also fails validation.

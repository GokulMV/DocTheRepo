<!-- dth:generated source="internal/adapters/llm/anthropic/anthropic.go" — edit only inside dth:human blocks -->
# `internal/adapters/llm/anthropic/anthropic.go`

<!-- dth:chunk 309f52683749d99e -->
## `Adapter`

Implements the ports.LLM interface for Claude. Maintains the API message and model services, a cache of models that rejected the effort parameter (in noEffort), and a fallbacks flag to enable server-side fallbacks on supported platforms.

<!-- dth:chunk d4b62d1de5dba26e -->
## `Adapter.effortUnsupported`

Reports whether a model is known not to accept the effort parameter. Checks a cache of models that previously rejected the effort parameter, then falls back to pattern matching for Haiku variants and Claude 3 models which ignore the parameter rather than failing.

<!-- dth:chunk 032bda3e959c312a -->
## `isEffortRejection`

Checks if an error is an Anthropic API 400 rejection of the effort parameter. Returns true only for HTTP 400 errors whose message contains "effort" (case-insensitive).

<!-- dth:chunk 9d9cdf7336472327 -->
## `Adapter.Chat`

Sends a Chat request to Claude, handling effort parameter gracefully by retrying without it if the model rejects it. Sets up prompt caching with cache control markers after the system prompt and final message. Leaves thinking at model default and never sends temperature. Supports JSON schema constraints and server-side fallbacks when enabled. Defaults max output tokens to 16000 if unspecified.

<!-- dth:chunk 023f10714b09ad68 -->
## `Adapter.send`

Performs a Claude API request with optional streaming for long or incremental generations. If no delta callback is provided and output fits below the streaming threshold, sends a single non-streaming request; otherwise streams the response, accumulating events and invoking onDelta for each text delta. Returns a permanent error if the stream contains no message_start event, indicating upstream streaming failure.

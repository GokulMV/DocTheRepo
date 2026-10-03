<!-- dth:generated source="internal/adapters/llm/pool.go" — edit only inside dth:human blocks -->
# `internal/adapters/llm/pool.go`

<!-- dth:chunk 8605a0c1730d945b -->
## `init`

Initializes the global provider registry with factory functions for all supported LLM and embedding providers. Registers providers including Anthropic, OpenAI, Azure OpenAI, Ollama, Bedrock, Vertex AI, GitHub Models, and specialized providers like Jev for decision-making and Opencode for documentation generation. Uses a helper `openAIish` to reuse OpenAI-compatible client creation logic across multiple providers that share the same interface.

<!-- dth:chunk 1ce66ea2cfea7e98 -->
## `withOpencodeDefaults`

Applies default configuration to Opencode provider by setting the command template to `dth engine opencode --model {model} {task_file} {result_file}` if not already specified. Creates a shallow copy of the extra configuration map to avoid mutating the input.

<!-- dth:chunk 9087b83bb558921e -->
## `__module__`

Defines module-level variables and constants: `regMu` and `registry` manage thread-safe access to the registered provider factories; `GitHubModelsURL` is the API endpoint for GitHub Models inference; `OpencodeCommand` specifies the default shell command template for the Opencode documentation generator; `ErrUnsupported` is returned when a provider doesn't support a requested capability (LLM, embedder, or doc generator).

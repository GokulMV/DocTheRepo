<!-- dth:generated source="web/src/pages/providerKinds.ts" — edit only inside dth:human blocks -->
# `web/src/pages/providerKinds.ts`

Defines TypeScript interfaces and configuration data for AI provider kinds that the Hub can integrate with, including their authentication, model, and feature requirements.

<!-- dth:chunk 364aeef736278ac0 -->
## `ExtraField`

Represents an extra configuration field for a provider kind. Fields include a key for internal reference, a label for display, and optional placeholder text, required flag, and hint for user guidance.

<!-- dth:chunk 32a8fdd00ea87c8d -->
## `ProviderKind`

Defines the configuration schema for a provider kind, including authentication (API key requirements and setup steps), base URL settings, model placeholders, embeddings support, optional extra fields, and chat features the provider can serve. The `key` field indicates whether an API key is required, optional, or not used; `baseURL` indicates whether it must be provided, is optional, or hidden from the user.

<!-- dth:chunk ea33c499056c9c4b -->
## `providerKind`

Looks up a provider kind by its string identifier. Returns the matching `ProviderKind` object or `undefined` if not found.

<!-- dth:chunk c3968180715b7098 -->
## `__module__`

Defines all supported provider integrations for the Hub, including Anthropic, OpenAI, Azure OpenAI, AWS Bedrock, Google Vertex AI, Ollama, OpenAI-compatible servers, GitHub Models, opencode, and others. Also defines the list of available chat features (docgen, QA, triage, etc.). Each provider entry specifies its authentication method, configuration requirements, model naming conventions, and supported capabilities.

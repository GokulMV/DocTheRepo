<!-- dth:generated source="web/src/pages/mcpCatalog.ts" — edit only inside dth:human blocks -->
# `web/src/pages/mcpCatalog.ts`

Defines the MCP (Model Context Protocol) service catalog structure and exports pre-configured integrations with cloud providers, monitoring tools, and collaboration platforms.

<!-- dth:chunk a94f22db4b521a33 -->
## `McpAuth`

Hosted MCP servers the Hub can connect to. Addresses and sign-in methods come from each vendor's documentation (checked October 2026); `check` marks entries whose exact address could not be confirmed, so the dialog asks the admin to compare it with the vendor's page. Everything stays editable.

<!-- dth:chunk 50b8fb461c336df7 -->
## `McpVariant`

Represents a variant option for an MCP server, with a display label and the corresponding server URL. Used for services offering multiple geographic regions or product variants (e.g., Datadog's regional endpoints, Grafana's stack address).

<!-- dth:chunk 335d1a9bc3e10e4f -->
## `McpEntry`

Defines the structure of an MCP (Model Context Protocol) catalog entry, describing a third-party service integration. Contains the service key, name, functional category, authentication method, endpoint URL, and optional variants for regional selection. Includes fields for placeholder values (like organization names), auth methods, API key naming conventions, documentation links, and operational notes. The `what` field briefly describes what the service exposes to Ask.

<!-- dth:chunk 2e62faba94bf427e -->
## `GCP`

Helper function that constructs a Google Cloud MCP endpoint URL by prepending the service name to the Google API domain pattern.

<!-- dth:chunk 5e9a709a075610c7 -->
## `mcpEntry`

Retrieves an MCP catalog entry by its key identifier. Returns the special `CUSTOM_MCP` entry for 'custom', otherwise searches `MCP_CATALOG` for a matching key, returning `undefined` if not found.

<!-- dth:chunk fe5365143c74dc50 -->
## `__module__`

Exports the complete MCP service catalog as `MCP_CATALOG`, a hardcoded array of supported integrations including AWS, Google Cloud, Azure DevOps, and monitoring/work platforms like Datadog, Jira, and GitHub. Also defines `CUSTOM_MCP` for user-provided servers and `AUTH_LABEL`, a lookup table mapping authentication types to human-readable descriptions.

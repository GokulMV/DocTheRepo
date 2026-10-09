<!-- dth:generated source="web/src/api/types.ts" — edit only inside dth:human blocks -->
# `web/src/api/types.ts`

Defines TypeScript types for API responses and data structures across documentation, MCP server integration, system architecture analysis, and conversational AI features.

<!-- dth:chunk 43c36371bf7c7ac9 -->
## `Me`

Represents the current user with identity, role, and access permissions. The `features` object indicates which optional features (issues, docs_v2, system) are available in the Hub based on configuration.

<!-- dth:chunk d59af12828b73043 -->
## `Citation`

A reference to a source used in an answer, with ordering (`n`), type classification, and metadata about the source location. The `chunk_id` uniquely identifies the source; `repo_id` and `path` provide code context when applicable.

<!-- dth:chunk 21fe2527b88b4ebb -->
## `AskResponse`

An answer from the Ask API including the response text, citations, token usage, and optional confidence assessment. The `cached` flag indicates if this was retrieved from cache; `sift` provides source filtering metadata.

<!-- dth:chunk fc28a11e2f4a6c12 -->
## `AnswerConfidence`

Represents the confidence level of an answer with a numeric score, categorical label, and optional reasons. The `why` array provides explanations for the confidence assessment.

<!-- dth:chunk 5d62db1911f404f0 -->
## `Message`

A message in a conversation thread with content, citations, and metadata. Includes optional confidence, sift summary, and user feedback. The `investigated` flag indicates the search was expanded due to insufficient initial results.

<!-- dth:chunk 11143fc327c8aa1e -->
## `McpTool`

Describes an MCP (Model Context Protocol) tool available from a server, including its name, optional title and description, and whether it is read-only.

<!-- dth:chunk ed7fc530335696f8 -->
## `McpServer`

Represents an MCP server connection that Ask can invoke, including authentication configuration, available tools, connection status, and error tracking. The `tool_choices` field indicates which tools are enabled; `status` tracks connection health with `needs_sign_in`, `ok`, `error`, or `new` states.

<!-- dth:chunk c75b117adfc04124 -->
## `ConfidenceLabel`

Type alias for confidence level categories: `'high'`, `'medium'`, or `'low'`.

<!-- dth:chunk a447af5d2d61224a -->
## `RepoDocSummary`

A Docs v2 document summary used in navigation, without expanded sections. Includes confidence scoring and status tracking. The `confidence` field is a number with a corresponding `label`; `changed` tracks Unix timestamp of last modification.

<!-- dth:chunk 84bba78d3b625f77 -->
## `RepoDocsList`

Response containing a paginated list of repository documents with optional budget and job status information. The `job` field tracks async generation progress with stage, counts, and errors.

<!-- dth:chunk 93ba85d22a04226b -->
## `RepoDocSection`

A section within a repository document, including its markdown content and confidence scoring for that section's accuracy and completeness.

<!-- dth:chunk ad08f9e4b74d59ec -->
## `RepoDoc`

A complete repository document with metadata, sections, and quality indicators. The `calibrated` flag indicates whether confidence scores have been validated; `source_sha` tracks the codebase version used for generation.

<!-- dth:chunk 9ea3c621c7106dc4 -->
## `SystemLink`

Represents a link between system components, tracking how repositories interact via events, APIs, function calls, libraries, or other mechanisms. The `n` field counts connection instances; `via`, `path`, and `line` provide source location details.

<!-- dth:chunk 01fcf768d1466f84 -->
## `SystemView`

A system architecture view containing inter-repository links, participating repositories, a diagram representation, optional generated documentation, and async job status. The `complete` flag indicates whether all relevant links were discovered.

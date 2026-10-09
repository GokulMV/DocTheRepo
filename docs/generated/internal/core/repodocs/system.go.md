<!-- dth:generated source="internal/core/repodocs/system.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/system.go`

Provides system-level documentation generation for multi-repository architectures.

<!-- dth:chunk 1e21dba88d3c914d -->
## `SystemRepo`

A repository viewed from the system level, containing its name, summary, architecture description, and a hash identifying the documents that comprise it.

<!-- dth:chunk 757ff3e89733334c -->
## `SystemInput`

Input data for system architecture generation: a collection of repositories, links between them, and a commit fingerprint for the record.

<!-- dth:chunk 9a6bbf1d3586929d -->
## `SystemHash`

Computes a hash of the system architecture's inputs (specification version, repository IDs and hashes, and all inter-repository links) to identify changes in the source material.

<!-- dth:chunk 089371cb9ae5ba3b -->
## `SystemDiagram`

Generates a Mermaid flowchart diagram showing repositories as nodes and their communication links as directed edges, labeled by kind and mechanism (e.g., "api: name"). Deduplicates edges, sorts them consistently, and truncates output at 60 edges to keep diagrams readable.

<!-- dth:chunk 676118413147f521 -->
## `clipLabel`

Truncates a string to 31 characters with an ellipsis suffix if it exceeds 32 characters; used for keeping diagram labels concise.

<!-- dth:chunk bd03d588fd62c111 -->
## `systemFacts`

Creates a synthetic Facts structure for system-level validation, with repository names as paths and links as known entities, allowing citation checks to work across the entire system using repository-qualified references.

<!-- dth:chunk 3ef17ceee9c7d7a1 -->
## `Generator.WriteSystem`

Generates system architecture documentation using LLM. Takes repository overviews, inter-repo links, and a mermaid diagram to produce a `Doc` with overview and section content. Constructs a material section from repository information and link data (showing communication patterns between repositories), then prompts the LLM via ChatJSONResult to generate structured output validated against a schema and checked for completeness. Preserves the document ID if updating a previous version, records token usage and cost, embeds the diagram in the interactions section, and scores the result using existing facts and link data. Returns error if routing fails or LLM call fails.

<!-- dth:chunk b782ac472674e15e -->
## `__module__`

Defines the system architecture specification: its audience (cross-service teams), sections (map, interactions, flows, contracts, coupling, and rules), and prompts the LLM to explain how repositories work together as a unified system.

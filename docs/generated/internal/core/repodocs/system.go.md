<!-- dth:generated source="internal/core/repodocs/system.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/system.go`

Defines system architecture documentation generation, including a flowchart diagram showing repository interactions and the specification for comprehensive system documentation sections.

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

Generates a Mermaid flowchart diagram showing repositories as nodes and their communication links as labeled edges. Creates unique node IDs for each repository, deduplicates edges based on source, destination, and label, and limits output to 60 edges to prevent diagram overcrowding. Link labels are enhanced for certain kinds (event, api, library, image, pipeline) by appending the `Via` field (clipped for length); other kinds use their name as-is. Repository and label names have quotes escaped for Mermaid syntax.

<!-- dth:chunk 676118413147f521 -->
## `clipLabel`

Truncates a string to 31 characters with an ellipsis suffix if it exceeds 32 characters; used for keeping diagram labels concise.

<!-- dth:chunk bd03d588fd62c111 -->
## `systemFacts`

Creates a synthetic Facts structure for system-level validation, with repository names as paths and links as known entities, allowing citation checks to work across the entire system using repository-qualified references.

<!-- dth:chunk 3ef17ceee9c7d7a1 -->
## `Generator.WriteSystem`

Generates the system architecture document by composing repository overviews and architecture descriptions with a Mermaid diagram, sending the material to an LLM with a validation loop to produce sections on the map, interactions, flows, contracts, coupling, and rules. Returns the completed document with usage metrics and status.

<!-- dth:chunk b782ac472674e15e -->
## `__module__`

Defines the specification for the "system" documentation section, targeting engineers and non-engineers across service boundaries. Comprises six subsections: a map describing repository roles and system cohesion; an interactions table and diagram explaining how repositories communicate via APIs, events, libraries, calls, images, and pipelines; end-to-end sequence diagrams (2-4 most critical) crossing services; shared contracts for producers and consumers; coupling analysis identifying risks and mismatches; and rules for safe cross-service changes. All subsections except contracts and dos require repository configuration or pipeline analysis as their source material.

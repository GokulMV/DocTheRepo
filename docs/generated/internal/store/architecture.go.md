<!-- dth:generated source="internal/store/architecture.go" — edit only inside dth:human blocks -->
# `internal/store/architecture.go`

<!-- dth:chunk e485bbf4816b703b -->
## `ArchNode`

A diagram node representing one box. EntityID links it to the Palace (empty for synthetic nodes like endpoint groups or dependency groups). Count and Items describe group boxes: Count is the number of entities aggregated, Items is the first few by name (up to 50). Kind is "endpoint_group", "dependency_group", "repo", or an entity kind.

<!-- dth:chunk 4af6b1f57730c381 -->
## `endpointGroup`

Extracts the resource group from an endpoint name by removing the HTTP method prefix (if present), stripping API version patterns, and returning the first path segment. For example, "GET /api/v1/connectors/{id}" yields "/connectors". Returns "/" if the path has no meaningful segment (empty, parameter-only, or malformed).

<!-- dth:chunk f0bf6273ae24abbe -->
## `ecosystemLabel`

Maps ecosystem identifiers to human-readable labels for group boxes in diagrams. Recognizes standard package managers (go, npm, Python, Java, Rust, Ruby) and returns language-specific plurals; unknown ecosystems are labeled as "*ecosystem* packages" and empty string becomes "Libraries".

<!-- dth:chunk 42919c413e0d5656 -->
## `isTestModule`

Identifies test and fixture directories by checking path segments (case-insensitive) against keywords like "test", "tests", "e2e", "mocks", "__tests__", etc. Returns true if any segment matches, distinguishing test code from the system itself.

<!-- dth:chunk b4319979fb4727e4 -->
## `architectureModules`

Filters the module list to only include architecturally significant boxes: excludes test modules and container directories that only hold other listed modules (like "internal" when "internal/core" exists). Uses the entity's key or name to determine directory.

<!-- dth:chunk 3e5328d21c77870f -->
## `Architecture`

One repository's generated architecture diagram with layers (upstream, interface, core, messaging, data, downstream). Includes the repository metadata, graph nodes and links, per-layer hidden counts, aggregate counts (endpoints, libraries), restricted count (links to invisible repos), environment variables, documentation references, owners, authored diagrams, and update timestamp.

<!-- dth:chunk 142fb984177cd794 -->
## `buildArchitecture`

Classifies the entity graph around one repository into architecture diagram layers and aggregates entities into boxes. Code-level entities (symbols, files) are lifted to services or repositories. Applies per-layer degree caps (anchor always included), drops links to removed nodes, counts endpoint groups and libraries, and sorts results. Handles local-to-local calls, downstream/upstream repositories, topics with multiple producers/consumers, datastores, and environment variables read.

<!-- dth:chunk 6059dedfa1a88dc2 -->
## `ArchitectureStore.Diagrams`

Fetches authored diagrams for a repository from the database, ordered by path. Scans each row into DiagramMeta and sorts them with sortDiagrams before returning. Returns error on query or scan failure.

<!-- dth:chunk 97334efb6e31d4de -->
## `sortDiagrams`

Orders diagram tabs with non-flow titles first, then flows ("Flow", "Sequence", "Step" prefixes) in natural numeric order. Uses stable sort, so equal-priority items maintain input order. Flow "A" precedes "Flow B", and "Flow 2" precedes "Flow 10".

<!-- dth:chunk 17a17f504d31cdfa -->
## `naturalLess`

Compares two strings treating contiguous digit sequences as numbers rather than lexicographically. Processes both strings in parallel: numeric runs are compared by magnitude then value, non-numeric characters by byte value. Returns true if a is less than b.

<!-- dth:chunk 53009922bcd329be -->
## `leadingDigits`

Extracts the leading run of ASCII digits from a string, returning the substring or empty string if none found.

<!-- dth:chunk a2eca70c2197ae1c -->
## `__module__`

Defines six architecture diagram layers (upstream, interface, core, messaging, data, downstream) with per-layer node caps (10 to 14 nodes), special node kinds for endpoint and dependency groups, regex pattern for API version prefixes, and sort order for layers. Maximum 20,000 edges per diagram.

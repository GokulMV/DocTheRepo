<!-- dth:generated source="internal/store/gen/models.go" — edit only inside dth:human blocks -->
# `internal/store/gen/models.go`

Defines data models for storing repository documentation in the database, generated from a schema or ORM tool.

<!-- dth:chunk 59326f827e361503 -->
## `Chunk`

Represents a distinct piece of source code (a function, type, class, etc.) with its metadata and content. Uniquely identified by `ChunkID` within a repository, it stores the code location (`Path`, `Symbol`), technical properties (`Language`, `ContentHash`, `Signature`), version control info (`CommitSha`, `Url`), and `RequiresRepos` listing dependencies. Supports soft deletion via `DeletedAt`.

<!-- dth:chunk 255d40c7879de26d -->
## `FileCard`

Represents a generated documentation card for a file, storing repository context and the card data structure. The `Card` field holds arbitrary JSON content representing different documentation shapes, with `UpdatedAt` tracking when the documentation was last regenerated.

<!-- dth:chunk 01e51dc983cee3f4 -->
## `McpServer`

Represents an MCP (Model Context Protocol) server configuration and runtime state. Stores connection details (`Url`, `CatalogKey`), authentication credentials (encrypted `SecretCiphertext`, `OauthCiphertext`), access control (`MinRole`), and operational status (`Status`, `LastError`, `CheckedAt`). The `Tools` and `ToolChoices` fields hold JSON arrays defining available tools and their configurations.

<!-- dth:chunk 987d46c176192378 -->
## `QaMessage`

Represents a message in a Q&A conversation thread, tracking both user queries and AI responses. Records the LLM provider and model used, token usage and costs, and optional feedback with comments. The `Citations` field holds JSON references to source documentation, and `Sift` and `Confidence` contain JSON analysis of result quality.

<!-- dth:chunk 23733c12f637b2a8 -->
## `RepoDoc`

Struct representing a repository documentation record stored in the database. Contains metadata about generated documentation including its type, content organization, quality metrics, and processing details. Fields like `Sections`, `Gaps`, `Why`, and `DraftProblems` store JSON data for flexible schema. Tracks generation costs (`TokensIn`, `TokensOut`, `CostUsd`), source integrity (`SourceSha`, `InputsHash`, `FileHashes`), model metadata (`Model`), and processing status (`Status`, `Error`). The `Calibrated` flag indicates confidence calibration, `Changed` tracks content modification percentage, and timestamps record creation and updates.

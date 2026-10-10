<!-- dth:generated source="internal/core/repodocs/facts.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/facts.go`

facts.go defines the core data structures representing what the Hub knows about a repository and the connections between repositories.

<!-- dth:chunk 0b0c2012ba0870dd -->
## `Symbol`

A declaration in the code, typically a function, type, or variable. It includes its name, kind (function, type, etc.), file path, line numbers, signature, and body content. The `In` field tracks incoming calls from other files to measure the symbol's centrality.

<!-- dth:chunk bcf00b1b35ff51db -->
## `Symbol.Exported`

Reports whether a symbol is exported (intended for public use) by checking naming conventions. For Go files, exports must start with an uppercase letter. For other languages, symbols are considered exported by default unless they start with underscore or are namespaced suffixes after `.`, `#`, or similar delimiters.

<!-- dth:chunk 5e9086dc728e047c -->
## `File`

A source file that the Hub indexed, containing its path, detected language, line count, and list of declarations. Both `Hash` and `Shape` fields fingerprint the file's content and structure respectively for change detection.

<!-- dth:chunk e2519ab6f2476f88 -->
## `Fact`

A piece of knowledge about the repository: endpoints, environment variables, datastores, topics, dependencies, ownership, services, or cloud resources. Each fact records its kind, name, location in code, and which symbol or file it belongs to.

<!-- dth:chunk 6b9c9be7f9f69b55 -->
## `Call`

Call is a call from one file to another (aggregated from symbol calls).

<!-- dth:chunk b302ae742beb6a98 -->
## `Import`

Import is a file importing a package of the same repository, by the package's directory (Go). Calls through values (a method on a struct field) are not resolved to their file, but the import is.

<!-- dth:chunk d3e2c99d157b6cc6 -->
## `Facts`

Facts aggregates all static information about a repository at a specific commit that the Hub collects without requiring a language model. It serves as the foundation for documentation generation, containing indexed source code elements (Files, Facts, Calls, Imports), file system metadata (AllPaths), special file contents (README, build/deploy/CI files in Special), diagrams found in the repository, and recent commits on the documented branch for change history and decision records tracking.

<!-- dth:chunk aa19430a134a23e1 -->
## `IsTest`

Reports whether a path is a test file by standard naming conventions (e.g., `_test.go`, `.test.`, `_spec.rb`) or directory structure (`test/`, `tests/`, `__tests__/`, `spec/`, `e2e/`, `testdata/`).

<!-- dth:chunk 294cbce916382ef2 -->
## `SpecialFile`

Reports whether a file should be read whole as context: READMEs, contribution guides, architecture docs, Docker configs, build files (Makefile, taskfiles), dependency manifests (package.json, pyproject.toml, etc.), ownership configs, CI/CD pipelines, deployment configs, and Helm charts.

<!-- dth:chunk a8a6cbd5030d5653 -->
## `Shape`

Creates a 16-character hex fingerprint of a file's structure by hashing each symbol's kind, name, and signature in order. Used to detect structural changes to a file independent of its full content.

<!-- dth:chunk d8fb142449ea7b81 -->
## `Hash`

Creates a 16-character hex fingerprint from an ordered list of strings, with null separators. Used for generic hashing of identifiers and content fragments.

<!-- dth:chunk 1cb284fb8a2cbc60 -->
## `Facts.FileByPath`

Returns a map from file path to file pointer for quick lookup of files by their path.

<!-- dth:chunk 567f58be061ee697 -->
## `Facts.Known`

Returns a set of names that documentation can safely reference in backticks: full and short symbol names, file and directory paths (from the indexed tree and all repository paths), and fact names. API endpoints are shortened to their path component (e.g., `/orders` from `POST /orders`). Matching is case-insensitive.

<!-- dth:chunk 1f101fb442c2c6ff -->
## `sortedKeys`

Returns a map's string keys in sorted order. Used internally as a utility for consistent iteration over maps.

<!-- dth:chunk 68bc8b16195869d2 -->
## `SystemLink`

SystemLink represents a single connection between two repositories, capturing how they communicate through various channels. It records the source and target repository/module names, the type of interaction (event, api, call, library, image, pipeline, or other), the specific mechanism (Via field), and optionally the file path and line number where the link originates. The N field likely counts occurrences of this type of link.

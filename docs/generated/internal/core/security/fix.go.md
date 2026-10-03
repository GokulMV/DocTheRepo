<!-- dth:generated source="internal/core/security/fix.go" — edit only inside dth:human blocks -->
# `internal/core/security/fix.go`

<!-- dth:chunk 5f06b45573b105d1 -->
## `Fix`

A proposed fix for a security finding, comprising complete rewritten source files, accompanying test files demonstrating the fix's correctness, and a risk note explaining the fixer's approach or limitations.

<!-- dth:chunk c803fd6e568c7225 -->
## `FixFile`

FixFile is one file with its complete new content.

<!-- dth:chunk baf7dc225b1f8947 -->
## `forbidden`

Determines whether a file path is forbidden from being modified by a fix, returning an empty string if allowed or a reason phrase if not. Prevents fixes from modifying CI configuration directories (.github, .gitlab, .circleci), lockfiles (*.lock, *-lock.json, go.sum, lock.yaml), absolute paths, and paths outside the repository.

<!-- dth:chunk 5ddba88c18ddf133 -->
## `Scanner.ProposeFix`

Requests a security fix from the LLM fixer for a given finding. Fetches the vulnerable file and nearby test files to establish coding conventions, validates the LLM response to ensure fixes don't write to forbidden paths, include complete content, and provide both vulnerability-closing and feature-preserving tests. Returns the proposed fix, token usage, or an error if no safe fix was proposed.

<!-- dth:chunk 46f94a73b803bb70 -->
## `testNeighbours`

Identifies likely test files for a given source file in priority order: suffix-based matches (_test, .test, .spec patterns) found in the provided tree, then any other test file in the same directory that follows naming conventions. Returns an empty slice if no test files are found.

<!-- dth:chunk 01601645024bc67e -->
## `__module__`

Defines constants and schemas for fix generation: MaxFixFileBytes (60 KB) limits file size for safe rewrites, fixSchema validates the LLM's JSON response structure (requiring files, tests, and risk fields), fileList defines the schema for file objects (path and content), and ErrNoFix indicates the fixer declined to propose a fix.

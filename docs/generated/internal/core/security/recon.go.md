<!-- dth:generated source="internal/core/security/recon.go" — edit only inside dth:human blocks -->
# `internal/core/security/recon.go`

<!-- dth:chunk 1416eb316e956a17 -->
## `moduleFocus`

A struct that defines how a security module identifies relevant files: a `paths` regex matches file paths directly, a `keywords` regex matches content words in code files to rank them, and the `code` flag indicates whether keyword matching applies to code files.

<!-- dth:chunk 60e7a620cc412e26 -->
## `Candidate`

Candidate is a file a module may read.

<!-- dth:chunk 956197899491a3b6 -->
## `Relevant`

Filters a file path to determine if it could be relevant to any module, returning false for vendored, generated, and test files (via `skipRE`), and true if the path has a recognized code extension, is a known manifest file, or matches any module's path pattern.

<!-- dth:chunk 9d8d27af73e38879 -->
## `PathCandidates`

Ranks relevant files from a repository tree by security module interest before reading content, scoring path matches (+3 points) higher than code files that might qualify (+1 point), sorting by score descending then path ascending, and returning at most `MaxFilesRead` candidates.

<!-- dth:chunk 0761812d269e6bf0 -->
## `ModuleFiles`

Selects files already read that are relevant to a specific security module: prioritizes path matches (+10 points), then scores code files by keyword hit count (up to +20 points), and returns sorted results within limits of `MaxFilesPerModule` files and `MaxModuleBytes` total bytes.

<!-- dth:chunk ea4cee8145eac8cb -->
## `__module__`

Defines resource limits for file analysis, supported code file extensions, a regex that excludes vendored/generated/test files, a set of known dependency manifest files, and a registry mapping ten security modules to their path and keyword matching patterns for reconnaissance.

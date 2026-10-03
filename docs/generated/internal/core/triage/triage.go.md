<!-- dth:generated source="internal/core/triage/triage.go" — edit only inside dth:human blocks -->
# `internal/core/triage/triage.go`

<!-- dth:chunk 8c50e6b541b44623 -->
## `Options`

Configuration for Triage. `DocsPath` specifies the generated-docs directory (changes there are never processed to prevent bot loops). `Ignore` and `IndexOnly` are glob patterns; nil values use defaults. `Keep` patterns (from "!" lines) re-include paths that Ignore matched.

<!-- dth:chunk d0fd2827100b0907 -->
## `Triage`

Classifies source code changes. Holds compiled regex patterns for ignore, keep, and index-only rules, plus a grammar registry for language detection and a docs path for bot-loop detection.

<!-- dth:chunk 5556df139e910992 -->
## `New`

Constructs a Triage instance. Compiles glob patterns for ignore, keep, and index-only rules into regexes; nil `Ignore` and `IndexOnly` use `DefaultIgnore` and `DefaultIndexOnly` respectively. Returns error if pattern compilation fails. The `DocsPath` is normalized by removing trailing slashes.

<!-- dth:chunk fb5efa39fcb87cf0 -->
## `Triage.ignored`

Returns true if path `p` matches any ignore pattern and does not match any keep pattern. The keep patterns act as exceptions to ignore patterns, implementing the "!pattern" logic from `.dthignore` files.

<!-- dth:chunk e207057d8c2132ac -->
## `Triage.File`

Classifies a single file change and returns a `Verdict` indicating whether to proceed with documentation updates. Aborts for: generated-docs path (bot-loop guard), ignored paths, standalone markdown files, or binary files. For deletions, proceeds with removal. For dependency manifests, applies special heuristics. For code files, parses both old and new content to detect structural changes, handling parse errors, language changes, and content-only modifications (comments/whitespace). Sets `NeedsLLM` true when no grammar exists.

<!-- dth:chunk 87048d12652f9811 -->
## `__module__`

Module-level constants and variables. `Abort` and `Proceed` are decision outcomes. `RenameSimilarity` (90%) is the threshold for considering a file a rename. `DefaultIgnore` excludes lock files, vendor dirs, minified/generated code, test snapshots, and config files. `DefaultIndexOnly` marks test files as index-only (not generating standalone docs).

<!-- dth:generated source="internal/core/repodocs/check.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/check.go`

Validates generated documentation against specifications, fact sources, and code material, checking sections, word limits, citations, and identifier references.

<!-- dth:chunk 9b7ad8a3d03b8b36 -->
## `DocSection`

A written section of documentation with its markdown content, quality score, and explanation for the score.

<!-- dth:chunk b801003b857237c1 -->
## `Doc`

A complete written document with metadata, sections, quality metrics (confidence, gaps), source information (file hashes, changed ratio, source SHA), and generation details (model, token usage, cost, status).

<!-- dth:chunk ed30af68e17f9e50 -->
## `Doc.Section`

Retrieves a section from the document by its key, returning a pointer to the matching DocSection or nil if not found.

<!-- dth:chunk 9849e90db51c81aa -->
## `Label`

Converts a confidence score (0–1) to a human-readable label: "high" (≥0.8), "medium" (≥0.6), or "low".

<!-- dth:chunk 62935835eeaca4d3 -->
## `Citation`

Citation is a [path:line] reference in a section.

<!-- dth:chunk 375af999faf7ca99 -->
## `Citations`

Extracts citation references ([path:line] and [path:line-end]) from markdown using regex, returning a slice of Citation objects with parsed file paths and line numbers.

<!-- dth:chunk c50e98c35618a9b2 -->
## `Words`

Counts prose words in markdown by removing code fences, tables, and citations, then counting fields that contain alphanumeric, unicode, or extended characters. Ignores pure punctuation.

<!-- dth:chunk c7d43a504b0be95f -->
## `identifiers`

Extracts backticked identifiers from markdown that appear to be code references rather than commands or expressions, filters out known identifiers and those found in the material, and returns the count and list of unknown ones. Strips code fences before processing, skips items containing shell operators or previously seen entries, and uses heuristics (underscores, dots, slashes, CamelCase, UPPER_CASE) to distinguish code names from prose. Trims common suffixes like `()` and `(…)` and extracting base names before function argument lists.

<!-- dth:chunk b450f109670d5dd5 -->
## `inMaterial`

Checks whether a name appears in the material (code, declarations, or prose provided to the model). Handles wildcard patterns by trimming trailing asterisks from the name and checking for prefix containment. Requires names to be at least 3 characters (after wildcard removal) to avoid false positives. Returns false for empty material.

<!-- dth:chunk f6f42ef1cce7aed4 -->
## `matchKnown`

Checks if a name matches a known identifier by testing the lowercase version, then checking for matches after colons (for file:line patterns), and after separators like dots, slashes, or hashes (e.g., Type.Method).

<!-- dth:chunk 58d37ba71d6f14aa -->
## `written`

Struct representing the model's generated response containing an at_a_glance summary and sections keyed by type with markdown content, plus identified gaps.

<!-- dth:chunk 1da7ecb178ddb0ff -->
## `checkResult`

Aggregates validation results from checking a reply: hard errors requiring fixes, citation counts and validity per section, identifier usage per section, unknown identifiers and unresolvable citations.

<!-- dth:chunk ff989f496a5dfcd1 -->
## `check`

Validates a generated documentation reply against its spec, facts, and provided material. Checks that at_a_glance is non-empty and under 140 words, all required sections are present and within word limits, citations resolve to actual files and line numbers (allowing file-level references at line 0 and non-indexed files by path alone), large claims have citations, identifiers are known or appear in material, and mermaid diagrams have recognized type headers. Collects hard errors that require fixes, plus per-section metrics on citations and identifiers. Material grounding allows config keys, environment variables, and similar names even without declarations.

<!-- dth:chunk eb04b90138d247e5 -->
## `first`

Returns the first n elements of a string slice, or the entire slice if shorter than n.

<!-- dth:chunk 46a2a15a6b8ef7f5 -->
## `containsStr`

Checks whether a value exists in a string slice.

<!-- dth:chunk 6a1c1dd634943897 -->
## `DocPath`

DocPath is where a document's sections live in the search index.

<!-- dth:chunk 7751cd1eb93f9be1 -->
## `Link`

Link is the document's address in the Hub.

<!-- dth:chunk c8f25dac359026fd -->
## `__module__`

Module-level regex patterns and lookup tables: `citeRE` matches [path:line] citations; `fenceRE`, `tableRowRE`, `tickRE`, `camelRE`, `mermaidRE` parse markdown structure; `mermaidKinds` lists valid diagram types; `commonTicks` filters out non-identifier keywords.

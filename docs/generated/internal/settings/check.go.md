<!-- dth:generated source="internal/settings/check.go" — edit only inside dth:human blocks -->
# `internal/settings/check.go`

<!-- dth:chunk 1fea0e8b199b0d23 -->
## `CheckOptions`

Configuration for the `Check` function. `Resolver` optionally verifies that every secret reference can be resolved; if set, references are expanded but values are discarded. `RequireOwner` enforces that the settings file explicitly names at least one owner, preventing unclaimed Hubs from being claimed by the first person to sign in.

<!-- dth:chunk 82c141fe564c3cd3 -->
## `CheckReport`

Result of validating a settings file. Contains extracted reference names and email addresses (never secret values), extracted booleans for SSO and password authentication status, a flag indicating whether all references were successfully resolved, and a list of validation problems found. All string slices are sorted.

<!-- dth:chunk 1e64674eae8ac222 -->
## `CheckReport.OK`

OK reports whether Check found no problems.

<!-- dth:chunk ce6b3885bfb3d6f6 -->
## `Check`

Validates a settings file for syntax errors, unknown keys, required fields, and owner presence without contacting a Hub. Parses YAML from all sources, extracts and resolves secret references (if a Resolver is provided), collects owners and admins from enabled users, and validates that user emails match SSO-allowed domains if configured. Returns parse errors as errors; validation problems (invalid emails, unresolved references, missing owner) are collected in the report.

<!-- dth:chunk 252c06823a2fc242 -->
## `walkValues`

Recursively traverses a YAML node tree, invoking `fn` on every scalar value (but not mapping keys). Handles document nodes and sequences by visiting all children, mapping nodes by visiting only values (skipping keys), and scalar nodes by calling the function directly.

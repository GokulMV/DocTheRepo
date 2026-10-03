<!-- dth:generated source="internal/core/triage/ignorefile.go" — edit only inside dth:human blocks -->
# `internal/core/triage/ignorefile.go`

<!-- dth:chunk 216a3e340470ebdd -->
## `ParseIgnoreFile`

Parses a .gitignore-style file into two glob lists: patterns to ignore and patterns to keep (re-includes). Lines starting with `#` or empty lines are skipped. A `!` prefix marks a keep pattern. Leading/trailing slashes and `\` escapes are processed: leading `/` anchors to root, trailing `/` matches only directories, and patterns without `/` use `**/` prefix for any-depth matching. Each pattern generates two globs: one for directory contents (`base/**`) and optionally one for the item itself (`base`) unless it's directory-only. Returns the ignore and keep glob slices.

<!-- dth:chunk 69dbd57b96a05fe9 -->
## `__module__`

Constant defining the filename for ignore patterns (.dthignore).

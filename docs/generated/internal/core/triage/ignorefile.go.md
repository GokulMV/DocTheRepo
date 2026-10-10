<!-- dth:generated source="internal/core/triage/ignorefile.go" — edit only inside dth:human blocks -->
# `internal/core/triage/ignorefile.go`

Provides functions to parse and match paths against .dthignore patterns with support for glob matching and re-inclusion rules.

<!-- dth:chunk c5b188167d192f62 -->
## `IgnoreMatcher`

Produces a path-matching predicate that reports whether a path should be ignored based on the patterns in a .dthignore file. The function parses the file content to extract ignore patterns (normal lines) and re-include patterns (lines starting with "!"), compiles them as globs, and returns a closure that matches paths against both sets—a path is ignored if it matches any ignore pattern and does not match any re-include pattern. Returns an error if glob compilation fails. Note that the built-in ignore list (which skips tests and CI files) is not included in the matching logic.

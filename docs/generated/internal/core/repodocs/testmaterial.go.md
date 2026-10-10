<!-- dth:generated source="internal/core/repodocs/testmaterial.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/testmaterial.go`

Extracts test names and excerpts from a module's test files for documentation, prioritizing by relevance.

<!-- dth:chunk 18c0ecb99e324df1 -->
## `TestName`

TestName is one test, suite or case declared in a test file.

<!-- dth:chunk 08aea96c8d083005 -->
## `isTestCode`

isTestCode reports a test file holding code (not fixtures or golden data under a test directory).

<!-- dth:chunk b9e8779451bd71dd -->
## `TestNames`

Parses test declarations from source code based on file language (Go, Python, JavaScript/TypeScript, Ruby, Java, Kotlin, .NET). Returns a list of `TestName` entries with line numbers. Recognizes Go Test/Benchmark/Fuzz/Example functions and t.Run cases; Python test_ functions and Test classes; JavaScript/TypeScript/Ruby describe/it/test blocks; and JUnit/Kotlin/.NET annotated test methods.

<!-- dth:chunk 7a5db00001bb6ca4 -->
## `testStem`

Extracts the canonical stem name of a test file (the name of the module it tests). Removes common test suffixes like `_test`, `.test`, `.spec`, `tests` and `test` from the filename, handling conventions across Go, Python, JavaScript, and Java. For example, `queue_test.go` → `queue`.

<!-- dth:chunk ae8d8cc955ece584 -->
## `testFile`

Container for a single test file being processed for module documentation, holding its file path, source code, line count, relevance score (weighted by size and call frequency of the files it tests), and the list of test names declared within it.

<!-- dth:chunk 3e5f113764fae745 -->
## `env.testMaterial`

Reads and processes a module's test files, prioritizing those that test the module's central files by weight (file size plus call frequency). Reads up to `maxTestFiles` files (skipping generated code and files over 512 KB), extracts test names and excerpts, and returns a formatted markdown block listing test names and excerpts with paths of unread test files.

<!-- dth:chunk 5b9ba090b6b556a2 -->
## `renderTests`

Formats test documentation by writing test names with line numbers (limited to `testNamesBytes`), followed by code excerpts from up to `maxTestExcerpts` test files sorted by relevance then size, and finally a list of test files not read. Each excerpt starts at the first test's line and is limited to `testExcerptLines` and `testExcerptBytes`.

<!-- dth:chunk 68953047ca0186bb -->
## `excerpt`

Extracts numbered lines from source code starting at line number `start`, up to `maxLines` lines, formatted with line numbers and left-padded. Truncates individual lines to 200 characters and stops when the total output would exceed `maxBytes`.

<!-- dth:chunk ab310ba5a4c4b5cd -->
## `clipLine`

Truncates a string to at most `n` characters, appending `…` if truncated. Returns the string unchanged if it fits within the limit.

<!-- dth:chunk 5d58938fc8691ada -->
## `__module__`

Constants and compiled regex patterns controlling test material extraction: limits on number of test files (10) and excerpts (8) per module, file size thresholds (512 KB), and total output sizes (~12,000 bytes for names and excerpts). Regexes match test declarations in Go, JavaScript/TypeScript, Python, and JVM/CLR languages.

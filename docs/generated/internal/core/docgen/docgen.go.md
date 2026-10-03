<!-- dth:generated source="internal/core/docgen/docgen.go" — edit only inside dth:human blocks -->
# `internal/core/docgen/docgen.go`

<!-- dth:chunk 522a19068d5ae34a -->
## `IsExternal`

IsExternal reports provider kinds that write docs through the DocGen contract (an agent CLI).

<!-- dth:chunk 9c777c94d8e52043 -->
## `Request`

Specifies a documentation generation request for changed chunks in a source file. It includes repository and commit metadata, paths to the source and generated documentation, and a list of targets (chunks to document). The `Feature` field selects which LLM route to use (docgen by default, or docgen_fast for faster processing of simpler code), while `Context` provides surrounding code needed for proper documentation.

<!-- dth:chunk b4ea2433d325fd2c -->
## `Generator.Generate`

Generates reference documentation for the requested targets by routing to an LLM (either external CLI or internal gateway), constructing a prompt listing all chunks to document with their change types, and validating the response contains documentation for each requested chunk with non-empty content. It returns a Result with file summary and sections in target order, or an error if validation fails or the LLM call fails. Routes to `g.external()` for external providers or uses `g.GW.ChatJSON()` for internal ones, dynamically capping output tokens based on provider limits.

<!-- dth:chunk 6528712d99c86bca -->
## `__module__`

Defines the system prompt used to instruct the LLM on generating reference documentation. It specifies the style and approach: writing for maintenance engineers, explaining what code does and why, documenting inputs/outputs/side effects where relevant, sizing documentation to code complexity (1-2 sentences for simple code, up to 150 words for complex), and including a one-sentence file summary. It also marks content in `<data>` tags as reference material, not instructions.

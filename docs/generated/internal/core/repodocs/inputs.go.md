<!-- dth:generated source="internal/core/repodocs/inputs.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/inputs.go`

Assembles and structures prompt material for documentation generation from repository analysis by collecting and prioritizing repository facts, code structure, and metadata.

<!-- dth:chunk 3d1901d7cebb28a5 -->
## `Reader`

Reader reads a file at the commit being documented (tests, migrations and CI files are not indexed).

<!-- dth:chunk 572526e0d1c8770b -->
## `Card`

A metadata card for a file that documents its purpose, structure, and the symbols it defines. Used internally as input material during document generation but never displayed directly. Contains the file path, a shape classification, a summary of purpose, optional notes, and a list of symbols defined in that file.

<!-- dth:chunk fe9f8781c0588a4f -->
## `CardSymbol`

CardSymbol is one declaration on a card.

<!-- dth:chunk 4c54561471cc76d2 -->
## `env`

The state and context for one documentation generation run, shared by all input builder functions. Holds facts about the codebase (symbols, files, calls), module information and structure (modules and their dependencies), user-provided cards describing files, cached module guides, a file reader, all symbols sorted by call frequency, and module-to-module dependency edge counts.

<!-- dth:chunk ced391a83602143e -->
## `newEnv`

Initializes a new environment by collecting symbols from all files, sorting them by call count in descending order, and computing inter-module dependency edges by filtering and counting function calls between different modules. Returns the initialized state ready for input generation.

<!-- dth:chunk f8fa51a21e5b84a7 -->
## `env.module`

Looks up a module by its key (identifier) in the environment's module list and returns a pointer to it, or nil if not found.

<!-- dth:chunk 0cc9e527a3c17d59 -->
## `numbered`

Formats source code with line numbers starting at `first`, limiting output to `maxLines` lines. Appends an ellipsis showing remaining line count if the input is longer. Enables the model to cite code locations by path and line number.

<!-- dth:chunk a1084afd826bc1a6 -->
## `clip`

Truncates a string to at most `n` characters. Returns the string unchanged if shorter; otherwise returns the first `n` characters followed by a newline and ellipsis.

<!-- dth:chunk aa43f5f9fca80f31 -->
## `block`

A titled section of prompt material with a priority level, used as a unit for including or excluding content when fitting text to a token budget.

<!-- dth:chunk 128ca151acee97de -->
## `block.tokens`

Estimates the token count of a block by dividing the combined character length of its title and body by 4.

<!-- dth:chunk d2a9fcee7bed0dc8 -->
## `render`

Assembles blocks into a prompt by inclusion priority until the token budget is exhausted. Outputs included blocks in priority order; if room remains after budget (more than 2000 characters), clips a lower-priority block to fit; otherwise drops it. Lists dropped sections at the end.

<!-- dth:chunk ca29fc90603b6cc3 -->
## `env.factsOf`

Filters facts by kind (e.g., "endpoint", "datastore") and returns them sorted alphabetically by name. Accepts multiple kinds and includes facts matching any of them.

<!-- dth:chunk 0e0ab56a341bd073 -->
## `factLines`

Formats a list of facts as a markdown bullet list, deduplicating by (kind, name, path) and limiting to `limit` entries. Each line shows the kind, name, optional source location if the path is set, optional context if the fact is from another symbol, and location details.

<!-- dth:chunk 560a670fcbc231f7 -->
## `env.moduleGraph`

Generates two representations of module dependencies: a text list of the top 80 calls between modules sorted by frequency, and a Mermaid flowchart diagram of the top 40 dependencies with module titles. Returns both the text list and diagram as strings.

<!-- dth:chunk 58e3ed3fcf34d335 -->
## `mermaidID`

Converts a module key into a safe Mermaid identifier by prefixing with "m_" and replacing hyphens with underscores.

<!-- dth:chunk 497d086443de699a -->
## `env.cardText`

Builds a human-readable summary of a file card by concatenating its purpose, symbol descriptions, and notes into a single string. Returns empty string if no card exists for the path.

<!-- dth:chunk ccff3d0a51c66b08 -->
## `env.bodies`

Formats the first `n` symbols from a list as markdown code blocks with their name, file path, line number, call count, and source code. Uses `numbered()` to display code with line numbers, limiting each body to `maxLines` lines.

<!-- dth:chunk 7af8a5b32e385e7c -->
## `env.special`

Formats all special files (such as README, build, deploy scripts) from the facts index as markdown code blocks, each clipped to `limit` characters with line numbers starting from 1. Files are sorted by key.

<!-- dth:chunk 5d69e061668d1b3f -->
## `env.readMatching`

Reads matching files from disk by applying a predicate function to all paths. Returns up to `maxFiles` files, each clipped to `perFile` characters, formatted as code blocks with line numbers. Silently skips files that fail to read. Returns empty string if no reader is available.

<!-- dth:chunk fbc7aeaa6da600f8 -->
## `env.moduleList`

Lists all modules with their metadata (title, file count, line count, test count) and a brief purpose summary drawn from module guides or file cards. Each module's purpose is clipped to 400 characters.

<!-- dth:chunk c9c4bac11e0446a7 -->
## `env.guidesText`

Formats module guides that were previously written, extracting the "at a glance" summary and specified section titles. Returns markdown for all guide sections requested, with content clipped to `perGuide` characters per guide.

<!-- dth:chunk 90b01709ca24fffe -->
## `env.entryPoints`

Identifies entry points (main functions, CLI entry files, server/app entry files) by pattern matching on symbol names and file paths. Returns a markdown list of discovered entry points, limited to 4000 characters.

<!-- dth:chunk 61df25dcd60e69e3 -->
## `env.testsText`

Summarizes tests by listing test file counts per directory (up to 120 directories), then reads a curated sample of test files: helpers and fixtures first, then one test per top-level directory. Returns the summary and sample code content clipped to size.

<!-- dth:chunk 44cfb0addca47750 -->
## `env.errorSymbols`

Finds symbols with names containing "err", "exception", or "fault" (case-insensitive). Lists their signatures and code bodies, showing up to 60 symbol names and 12 complete bodies, each with 30 lines max.

<!-- dth:chunk c8550d67ce47cc85 -->
## `env.inputs`

Assembles prompt material for document generation by building a list of content blocks based on a specification. Each item in `spec.Needs` triggers collection of specific repository information—modules, dependency graphs, declared facts (endpoints, datastores, topics), entry points, tests, CI configuration, decisions, and more. The blocks are prioritized (lower numbers appear earlier) and passed to `render()` which selects them to fit the given token budget.

<!-- dth:chunk 4861bde80838196e -->
## `env.titleOf`

Returns the title of a module by key, or the key itself if no such module exists.

<!-- dth:chunk 0d637ff8f002d26c -->
## `env.moduleTests`

Lists each module with its test file count as a simple markdown bullet list.

<!-- dth:chunk 866ed6be4e821aeb -->
## `env.handlerBodies`

Extracts the first `n` handler/producer/consumer function bodies associated with a fact list by matching the fact's `From` field to symbol names. Returns their bodies formatted with line numbers, limiting each to 50 lines.

<!-- dth:chunk 6297454cd3d6f165 -->
## `env.moduleInputs`

Populates a module-specific document with five sections: basic metadata (file count, line count, directory), a file listing with line counts and summaries, exported declarations sorted by usage frequency (capped at 120), facts this module declares (endpoints, environment variables, datastores, topics), and the module's most-called internal code. If test files exist, includes test material; otherwise notes their absence. The `add` callback registers each section with a priority level.

<!-- dth:chunk 888f8d4405590ff4 -->
## `commitLines`

Formats a list of commits as a markdown bullet list, showing up to `n` commits with date, abbreviated SHA, author, and message. If `full` is false, only the first line of each message is shown; otherwise the entire message is included. Each message is clipped to 800 characters.

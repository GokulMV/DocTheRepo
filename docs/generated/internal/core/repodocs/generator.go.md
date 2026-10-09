<!-- dth:generated source="internal/core/repodocs/generator.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/generator.go`

Generator orchestrates LLM-powered creation and updating of repository documentation, managing costs, batching, and API routing.

<!-- dth:chunk 547f8c5b25080877 -->
## `Store`

**Store** is an interface for persisting documentation artifacts. It manages retrieval and storage of cards (file summaries for indexed code) and docs (generated documentation), and removes outdated documents when the catalog changes.

<!-- dth:chunk 59b0706cb3d8ce7b -->
## `Generator`

Coordinates writing repository documentation through an LLM. `GW` routes LLM calls; `Store` persists results. `Cost` and `CostUsage` are optional functions for pricing calls (the latter handles prompt-cache token rates separately). `Parallel` controls document concurrency (default 3), `Budget` limits input tokens per document (default 60000), `Rewrite` is the source-change threshold to rewrite unchanged structure (default 0.3), `Types` filters which document types to generate, and `Now` provides timestamps.

<!-- dth:chunk 0af577e1f0c5f6ce -->
## `RunOptions`

**RunOptions** specifies inputs for a single documentation run. Meta carries LLM call metadata; Read provides facts and file content; Force rewrites all documents; Only restricts which documents to rewrite; DryRun estimates without writing; RetryFailed rewrites previously failed documents; Progress reports completion; Guard runs before paid steps and can halt on budget exhaustion.

<!-- dth:chunk 31c5f6e3f046062d -->
## `Result`

**Result** reports the outcome of a documentation run, including module and document counts, tokens consumed, cost, and lists of written, unchanged, failed, and removed documents. For dry runs, it contains estimates (WouldWrite, EstimatedTokens, EstimatedUSD). Changed holds documents written or removed in this run for indexing.

<!-- dth:chunk 96ebd8d2a08f87e0 -->
## `Generator.now`

Returns the current time using the generator's clock function if set, otherwise the system time. Allows injection of time for testing.

<!-- dth:chunk 903a685bd1fd7111 -->
## `job`

**job** represents a single document to consider for generation. It holds the document specification, the module it applies to (nil for repo-level docs), a key, a hash of structural inputs, and the source files used to detect changes.

<!-- dth:chunk 47b0184c2a5e1f4e -->
## `job.id`

Returns a unique document identifier by combining its type and key (e.g., "module/core", "architecture/architecture").

<!-- dth:chunk faff8b0d1d64254e -->
## `Generator.Run`

**Run** orchestrates document generation for a repository. It splits the codebase into modules, writes or updates cards for changed files, identifies which documents need generation (by checking hashes and source changes), and processes them in parallel (module docs first, then repo-level). It also deletes obsolete documents and returns summary statistics. Respects the Guard and Progress callbacks, handles dry runs with token estimates, and integrates cost tracking.

<!-- dth:chunk f24db71758049731 -->
## `Generator.wanted`

Checks whether a document type is in the generator's Types filter (all types if filter is empty).

<!-- dth:chunk 8ee71a7c9f7c58b5 -->
## `docTitle`

Returns a human-readable title for a job's progress display: the spec title for repo-level docs, or the spec title plus module title for module docs.

<!-- dth:chunk a44cbf318c62b04d -->
## `Generator.needs`

**needs** decides if a document should be written. It returns true if forced, missing, previously failed (and RetryFailed is set or inputs changed), in the Only filter, or has a changed input hash. If the hash is unchanged, it checks if the rewrite threshold (default 30%) of source lines changed and returns true if exceeded.

<!-- dth:chunk aea609573a53e613 -->
## `changedShare`

Computes the fraction of a document's source lines that changed since generation. It compares file hashes stored in the document with current hashes; returns 0 if no hashes were stored. Returns the ratio of changed lines to total lines in those source files.

<!-- dth:chunk 45e6ffa9cc5aeccc -->
## `Generator.moduleHash`

**moduleHash** computes a structural hash for a module guide covering its files' shapes, declared symbols, tests, and neighbours in the dependency graph. Changes only to function bodies leave it unchanged, allowing incremental rewrites only when structure or dependencies change.

<!-- dth:chunk 33808ed07735d406 -->
## `Generator.repoHash`

**repoHash** computes a structural hash for a repository-level document. It combines the spec type, module list, and structural inputs from the Needs list: facts (symbols), module graph edges, special files, tests/CI/migrations, commit weeks, decision commits and ADRs, and top symbols. Used to detect when to regenerate.

<!-- dth:chunk e9da8ba2b2ca9649 -->
## `sourcesFor`

Returns all repository file paths as sources for a document spec. Used for change detection on repo-level documents.

<!-- dth:chunk a93d119d693eba18 -->
## `Generator.write`

Generates a single document by prompting the LLM with collected source material, validating the response with checks (one repair attempt), and scoring confidence. Creates a `Doc` with metadata, file hashes, and token/cost tracking. Handles architecture diagrams by prepending a mermaid graph. Returns the completed document or an error if the LLM call failed.

<!-- dth:chunk 1ba493c81d28219e -->
## `prompt`

Constructs the user message for document generation. It specifies the repo, document type, audience, purpose, and lists each section with its word limit, requirement level, and writing guidance, then appends the material in tags.

<!-- dth:chunk 2c1945304b068ecf -->
## `Generator.score`

**score** assigns confidence and why-notes to a document and its sections. It combines grounding (valid citations to code), names (backticked symbols that exist), and optionally support (decision model's judgment of whether citations back claims). Section scores use these weights: 65% grounding + 35% names, or 40%+20%+40% with support. Document confidence is the weakest required section, lowered for uncovered module declarations and gaps.

<!-- dth:chunk 77112c62a61560f8 -->
## `round2`

Rounds a float to 2 decimal places for confidence and score display.

<!-- dth:chunk fdfac16c38238ba5 -->
## `coverage`

Computes what fraction of a module's top (up to 12 most-used exported) declarations the guide mentions. Searches section markdown for the symbol names and returns the hit ratio and list of missing names. Guides that miss key declarations get lower confidence.

<!-- dth:chunk 8fd95694b3bda0ff -->
## `Generator.support`

**support** uses the decision model to judge whether a section's cited code actually supports its claims. It collects snippets around each citation (up to 8), asks the model to assess support level (supported/partly/unsupported), and returns the combined probability and calibration flag. Returns zero score if no citations are found or the model is not routed.

<!-- dth:chunk ab29a6b21afc5c6c -->
## `env.snippet`

Extracts a code snippet around a citation. It finds the symbol containing the cited line, extracts context (6 lines before and 12 after), numbers the lines, and returns at most 40 characters per line. Returns empty string if the file or line is not found.

<!-- dth:chunk 26b81c56e6984679 -->
## `Generator.writeCards`

Generates brief descriptions (cards) for files whose code structure changed, processing them in batches up to 24KB or 12 files each. Uses a fast model if available, otherwise falls back to the standard model. Runs up to 4 batches concurrently with spend-guard checks and progress callbacks. Stores results and updates the cards map; returns count of successfully written cards.

<!-- dth:chunk c41905ae6e3a877b -->
## `cardInputSize`

Estimates input tokens for a file's card generation: base 200 plus signature and body (capped at 1200) of each symbol, divided by 4 for token approximation, capped at 6000.

<!-- dth:chunk 80300d6d1b4da024 -->
## `Generator.cardBatch`

Produces `Card` objects for a batch of files by sending their symbols and code excerpts to the LLM. Truncates symbol bodies to 1200 characters and respects a per-file budget to stay within token limits. Validates that all files received cards and filters out any unexpected paths from the response. Returns cards with shape matching the input files.

<!-- dth:chunk 9d025b818b5c1807 -->
## `MarshalSections`

MarshalSections is used by stores that keep sections as JSON.

<!-- dth:chunk 9aecf74849f2ba37 -->
## `inputTokens`

inputTokens is all the input a call was billed for: uncached, cache reads and cache writes.

<!-- dth:chunk e23d72d889f5bf77 -->
## `Generator.usageCost`

Calculates the cost of an LLM API call, preferring detailed cache-aware rates when available. If `CostUsage` is set, it uses all token types including cache reads/writes; otherwise falls back to `Cost` with just input/output tokens. Returns 0 if neither cost function is configured.

<!-- dth:chunk 2fe320d66c9bf259 -->
## `__module__`

Module-level constants: **system** is the prompt preamble for document generation (rules on truthfulness, citations, format); **docSchema** is the JSON schema for document output (at_a_glance, sections array, gaps); **cardSchema** is the schema for file cards; **cardSystem** is the prompt preamble for card generation.

<!-- dth:generated source="internal/core/sift/sift.go" — edit only inside dth:human blocks -->
# `internal/core/sift/sift.go`

<!-- dth:chunk b6b601fa7228243b -->
## `Cache`

Cache stores per-piece judgments; Memory is the default.

<!-- dth:chunk 08c597032dada085 -->
## `Score`

Score is one piece's judgment.

<!-- dth:chunk d427a6974ad12a94 -->
## `Sifter`

Sifter orchestrates evidence selection by judging chunks against a question using an LLM judge. It holds configuration for the judging process: Gateway for making judge calls, a Cache for storing judgments, a Cost function for economics checks, and tuning parameters (KeepAt threshold, Batch and Parallel sizes, Excerpt length, MinTokens filter, CostRatio cap). The zero value of each tuning field uses its default constant.

<!-- dth:chunk 2f31f2ea0cdbddfa -->
## `Report`

Report captures the outcome of a sift operation: counts of candidates, kept, judged, and cache hits; metadata about the judge route and model; token usage; and per-chunk Scores. It includes flags for whether judging was calibrated or weak (threshold not met), a skip reason if sifting was skipped, and metrics for Navigate operations if applicable.

<!-- dth:chunk ad191906668312b5 -->
## `Sifter.keepAt`

keepAt returns the configured keep threshold, defaulting to DefaultKeepAt if not set.

<!-- dth:chunk e7579379a900fda6 -->
## `Sifter.batch`

batch returns the batch size for judge calls, defaulting to DefaultBatch if not set.

<!-- dth:chunk e49a69d23f1a0b6e -->
## `Sifter.parallel`

parallel returns the number of concurrent judge calls to make, defaulting to DefaultParallel if not set.

<!-- dth:chunk 59b6721ec283b60d -->
## `Sifter.excerpt`

excerpt returns the character limit for chunk content shown to the judge, defaulting to DefaultExcerpt if not set.

<!-- dth:chunk 562f77fcbc94e2fe -->
## `Sifter.minTokens`

minTokens returns the minimum token threshold for candidates to be worth judging, defaulting to DefaultMinTokens if not set.

<!-- dth:chunk c9f4cf481ceda617 -->
## `Sifter.costRatio`

costRatio returns the cost threshold for judge input pricing relative to the answer model, defaulting to DefaultCostRatio if not set.

<!-- dth:chunk 5155f5b4c9ed18b9 -->
## `or`

or returns v if positive, otherwise returns default d, used to apply default values when a config field is zero.

<!-- dth:chunk afe8ea8ab5e053d4 -->
## `Sifter.Worthwhile`

Worthwhile determines if using a separate judge model is cost-effective by comparing judge and answer route costs. It returns the judge route and a reason string; a non-empty reason means judging is not worthwhile (e.g., no judge route available, same model for both, or judge input cost exceeds the cost ratio threshold relative to the answer model).

<!-- dth:chunk 4cbb792c1d688417 -->
## `Sifter.Select`

Select judges all chunks against a question and returns those meeting the threshold (relevance and scope ≥ keepAt/2), sorted by relevance with ties preserving retrieval order. It performs early exits if no candidates, insufficient tokens, judging is not worthwhile, or judging fails. If nothing passes the threshold (weak), it returns the top few by relevance (fallbackKeep) so the answering model can still respond. Report captures all metrics: candidates, kept, judged, cache hits, calls, token usage, and per-chunk scores.

<!-- dth:chunk 98899599d3825097 -->
## `Sifter.keep`

keep applies relevance >= keepAt and scope >= keepAt/2 thresholds to filter chunks. If no chunks pass (weak case), it sorts all by relevance descending (with ties preserving retrieval order) and returns the top few (fallbackKeep). Otherwise it returns the passing chunks in the same sort order.

<!-- dth:chunk 23b2365809be8d6c -->
## `Sifter.score`

score judges all chunks against a question, returning relevance and scope scores per chunk ID. It first checks the cache for existing judgments, then batches any uncached chunks through the judge via judgeAll, caches new results, and accumulates usage metrics in the report.

<!-- dth:chunk 71bec64b736e78ce -->
## `item`

item represents a single piece the judge reads: source code, a file preview, or directory listing. It carries kind (code/documentation/etc.), scope (repository), path, optional symbol, text content or directory entries, and file count for directories.

<!-- dth:chunk 3fac89edeb157f28 -->
## `questionsFor`

questionsFor turns item i into its yes/no questions; every question set has a fixed length.

<!-- dth:chunk 0ec71484bdab717d -->
## `pieceQuestions`

pieceQuestions generates two yes/no judge questions for item i: one assessing relevance against the criteria, one assessing scope. Each question ID is indexed by item and question type (rel or scope).

<!-- dth:chunk 5eb4adf87ac900ba -->
## `Sifter.judgeAll`

judgeAll asks questions for all items, batching Batch items per call and running Parallel calls concurrently. It constructs a JSON state with question, guidance, criteria, and items, then submits to the judge in parallel batches. Returns a 2D array of probabilities (one row per item, one column per question) in question order, or an error if any call fails.

<!-- dth:chunk f99c9a5dd06b35ff -->
## `cacheKey`

cacheKey computes a SHA256 hash combining prompt version, judge route (provider ID and model), question fingerprint, chunk ID, and chunk content. Used as the cache lookup key for judgments.

<!-- dth:chunk a24dd67a34525fa2 -->
## `fingerprint`

fingerprint normalises a question for the cache: case and spacing do not change a judgment.

<!-- dth:chunk 64a4a5a044b669de -->
## `kindOf`

kindOf maps a chunk's source type to a human-readable category: code, issue explanation, team document (for Confluence/Jira/Notion/upload), or documentation (default).

<!-- dth:chunk 2e68ac45a17d3171 -->
## `symbolOf`

symbolOf returns the chunk's symbol for the judge, or empty string if it is a module symbol or starts with __ (indicating synthetic/generated symbols).

<!-- dth:chunk 443e151e0b17a4e8 -->
## `clip`

clip truncates string s to n characters on a UTF-8 rune boundary and appends a marker. If s is already ≤ n bytes, it returns unchanged. Otherwise it backs up from position n to avoid splitting a multi-byte rune, then appends "\n…(excerpt ends)".

<!-- dth:chunk 0e2e94a0c28d5fcb -->
## `Memory`

Memory is a bounded in-process cache for judgment scores, evicting oldest entries first when the size reaches Max (default 20000). It is thread-safe and implements the Cache interface.

<!-- dth:chunk 98445a777d5bf974 -->
## `Memory.Get`

Get retrieves a cached score by key, returning (score, true) if found, else (zero, false).

<!-- dth:chunk d4f8ff885c44d5c1 -->
## `Memory.Put`

Put stores a score in the cache, tracking insertion order. When the cache exceeds Max entries (default 20000), it evicts the oldest entry. Lazily initializes the map on first call.

<!-- dth:chunk 57bb3396fd64272a -->
## `__module__`

Module constants define prompt version (sift-1), defaults for all Sifter tuning fields, and the judge criteria and guidance text sent to the LLM.

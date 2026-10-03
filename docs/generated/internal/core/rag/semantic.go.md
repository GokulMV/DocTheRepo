<!-- dth:generated source="internal/core/rag/semantic.go" — edit only inside dth:human blocks -->
# `internal/core/rag/semantic.go`

<!-- dth:chunk 0d84ef7001118b5e -->
## `SemanticCache`

SemanticCache is a Store that can also find a cached answer by the meaning of the question: same scope and embedding model, embedding similarity at least minSim, sources unchanged. Optional.

<!-- dth:chunk 989a4ef8e4b7d2d0 -->
## `ScopeKey`

Generates a cache partition key from a Scope by hashing its normalized components (all flag, sorted repository IDs, and sorted sources). Returns a hex-encoded SHA256 digest, ensuring consistent keys for identical scopes regardless of input order.

<!-- dth:chunk cd0bb645701d90a6 -->
## `Cosine`

Computes cosine similarity between two float32 vectors, returning 0 if lengths differ, either vector is empty, or either vector has zero magnitude. Performs the calculation using double precision arithmetic for accuracy.

<!-- dth:chunk b0a1d1a61edfee43 -->
## `Identifiers`

Extracts code-specific identifiers (file paths, dotted names, snake_case, CamelCase, and words with digits) from a question string by filtering regex matches. Returns a sorted list of lowercase identifiers to distinguish between semantically similar names like "PayRetry" and "CartRetry" that embeddings might conflate.

<!-- dth:chunk c624437e81cc4f58 -->
## `sameIdentifiers`

sameIdentifiers reports whether two questions name the same code: every identifier in either one appears, ignoring case, among the other's words.

<!-- dth:chunk 310a524f549409b8 -->
## `namesIn`

Checks whether all identifiers found in the `from` string appear in the `in` string. Returns true if `from` contains no identifiers or all of its identifiers are present in `in`; used to verify semantic cache eligibility by ensuring answer source identifiers match the question.

<!-- dth:chunk cacec6741a03bec2 -->
## `__module__`

Defines the default cosine similarity threshold (0.95) for reusing cached answers and compiles a regex pattern to match code identifiers: sequences starting with a letter or underscore, containing word characters/dots/slashes/hyphens, and ending with a word character.

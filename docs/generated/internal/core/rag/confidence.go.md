<!-- dth:generated source="internal/core/rag/confidence.go" — edit only inside dth:human blocks -->
# `internal/core/rag/confidence.go`

Implements confidence scoring for RAG answers based on citation coverage, source trustworthiness, and source diversity.

<!-- dth:chunk 3e93036b79ec8751 -->
## `Confidence`

Represents the confidence level of an answer with a numerical score (0–1), categorical label (high/medium/low), and optional reasons explaining the rating.

<!-- dth:chunk b1f72cfaa5fcf311 -->
## `sourceTrust`

Assigns a trust weight to a source chunk based on its type: code and tool results are most trusted (0.95, 0.9), written pages and generated documents receive lower scores (0.8), and generated documents are further penalized by their own internal confidence level. Returns both the trust score and an optional explanatory reason.

<!-- dth:chunk 603143b3f933718d -->
## `Score`

Rates an answer's confidence by combining citation coverage (how many sentences cite sources) and source trust, with penalties for weak source selection or single-source answers. Citation coverage is weighted 45%, source trust 55%; scores are clamped to [0,1], rounded to two decimals, and labeled as high (≥0.8), medium (≥0.6), or low. Returns nil if no citations exist or the answer is a not-found response.

<!-- dth:chunk 833bc2ec6e6b57ce -->
## `stripCode`

Removes code blocks (delimited by triple backticks) from a string by replacing them with spaces, used to exclude code content from citation coverage analysis.

<!-- dth:chunk 773076af46911682 -->
## `__module__`

Module-level regular expressions for parsing answer structure: `sentenceRE` splits text into sentences, `citedRE` identifies citations by pattern `[N]`, `docConfRE` extracts confidence levels from generated document markers, and `fenceStripRE` matches code blocks for removal.

<!-- dth:generated source="internal/core/repodocs/report.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/report.go`

Defines data structures and functions to summarize and report on document generation runs, including problem aggregation, quality metrics, and Markdown rendering.

<!-- dth:chunk a7136fa5da91bde1 -->
## `Report`

A `Report` summarizes the outcome of a document generation run, including counts of successful and failed documents, token usage, cost, and quality metrics. The `Confidence` field stores the average confidence score across successful documents, while `Labels` tracks how many documents fall into high/medium/low confidence bands. `Problems` aggregates draft issues by kind with their frequency, and `Items` holds detailed results for each document.

<!-- dth:chunk 655ee28877004043 -->
## `ProblemCount`

A `ProblemCount` groups occurrences of a single kind of draft problem with an example instance. The `Kind` field contains the generalized problem pattern (with specific values stripped), `N` is the occurrence count, and `Example` provides a representative instance for inspection.

<!-- dth:chunk 45300618077cb182 -->
## `ReportItem`

A `ReportItem` holds detailed results for one generated document, including its status, error (if failed), token counts, cost, and confidence score. It tracks quality assessment through `Label`, calibration status, and arrays of issues (`Why`, `Gaps`, `DraftProblems`). `Sections` contains per-section analysis, `Missing` lists required sections that were not written, and `AtAGlance` (when text is included) provides a summary.

<!-- dth:chunk 6548b662687fa3f5 -->
## `ReportSection`

A `ReportSection` measures one section's performance: `Words` is its actual length, `Target` is the ideal length (0 means no target), and `Length` categorizes it as "ok", "long", or "short" based on a ±60% band around the target. `Score` (0–1) rates the section's quality with explanations in `Why`. `Markdown` optionally stores the section text when included in the report.

<!-- dth:chunk d802d2e07052af9a -->
## `SpecFor`

Finds a document type specification in the catalogue by matching its `Type` field. Returns the matching `Spec` and `true`, or an empty `Spec` and `false` if not found. Checks `SystemSpec` first, then iterates the `Catalog`.

<!-- dth:chunk 11344290190f8833 -->
## `BuildReport`

Aggregates document generation results into a summary report. Accumulates token usage, cost, and model names, then builds a `ReportItem` for each document with its sections. Categorizes sections as long/short/ok based on word count against targets. Groups draft problems by kind (with specifics stripped), counts confidence labels, and calculates average confidence for successfully generated documents. When `text` is true, includes section Markdown and document summaries in the report.

<!-- dth:chunk 88a0818093f46876 -->
## `problemKind`

Normalizes a problem string by replacing specific values (backtick-quoted code, bracketed citations, quoted strings, path expressions, and numbers) with placeholder tokens. Also generalizes section-specific prefixes by replacing them with `<section>`. Truncates to 140 characters if needed. This allows the same underlying issue (e.g., "missing function signature") to group together despite varying details.

<!-- dth:chunk a05371e5573db62d -->
## `Report.Markdown`

Renders the entire report as Markdown for reading or pasting into an issue. Outputs a summary table with document counts, confidence distribution, costs, and models. Then lists draft problems by frequency, failed documents, weak sections (score < 0.6), sections off-target length, and a per-document summary. If text was included in building the report, also shows each document's full sections with Markdown prose and any gaps reported by the model.

<!-- dth:chunk dcafe5e1631c91b7 -->
## `hasText`

Returns true if the `ReportItem` has any section with non-empty Markdown text, used to decide whether to include its full details in Markdown rendering.

<!-- dth:chunk ed7cd80aa8a8ba57 -->
## `cell`

cell makes text safe inside a Markdown table cell.

<!-- dth:chunk 7caa68be5ac31a2a -->
## `__module__`

Package-level regular expressions used by `problemKind` to identify and normalize specific values in problem messages: backtick-quoted strings, bracketed citations, quoted strings, numbers, and path expressions (dollar signs followed by field accessors or array indices).

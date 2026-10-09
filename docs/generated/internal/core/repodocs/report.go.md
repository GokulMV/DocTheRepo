<!-- dth:generated source="internal/core/repodocs/report.go" — edit only inside dth:human blocks -->
# `internal/core/repodocs/report.go`

Defines Report structures and aggregation logic to summarize documentation generation results and quality metrics.

<!-- dth:chunk 11344290190f8833 -->
## `BuildReport`

Aggregates documentation metrics and quality data from individual docs into a unified report. Accumulates token usage, cost, and model counts across all docs; classifies each doc by status (failed/ok) and confidence level; identifies missing required sections and length anomalies by comparing actual word counts against section targets (flagging sections >1.6× or <0.4× target as "long"/"short"); tracks draft problems by kind with examples; optionally includes full Markdown and at-a-glance text when `text` is true. Returns report with sorted models and problems ranked by frequency.

<!-- dth:generated source="web/src/pages/Analytics.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Analytics.tsx`

<!-- dth:chunk 6b7129c7e9f875ea -->
## `Analytics`

Renders an analytics dashboard displaying LLM usage metrics, cost savings, and pipeline statistics over a configurable time period. Users can group usage data by feature, provider, model, repository, or user, and toggle between tokens, cost, and calls metrics. The component fetches usage data via `useUsage`, savings breakdown via `useSavings`, and pipeline stats via `usePipelineStats`, then transforms the usage time-series into a stacked bar chart organized by the selected grouping. Includes summary statistics (calls, tokens, cost, savings) at the top, a bar chart of usage over time, a savings breakdown table, and a pipeline performance section showing job latency percentiles and documentation freshness by repository.

<!-- dth:chunk 1a89d81ec612069e -->
## `__module__`

Exports module-level constants: `COLORS` is a palette of seven hex color codes for chart series visualization; `SAVINGS_LABEL` is a lookup table mapping internal savings event types (e.g., `triage_abort`, `answer_cache_hit`) to human-readable labels displayed in the savings table.

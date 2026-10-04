<!-- dth:generated source="web/src/pages/Analytics.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Analytics.tsx`

Analytics dashboard providing LLM usage visibility, cost tracking, savings attribution, source picker efficiency, and repository documentation pipeline health.

<!-- dth:chunk a0dbc7e1d631bfe8 -->
## `SiftCard`

Displays a card showing analytics for the Ask source picker—a cheap judge that trims retrieved sources before sending to the answering model. Shows loading state or a message if no source picking occurred in the period. When data exists, displays key metrics including tokens not sent, net savings after judge cost, source read ratio, and judge token usage. If more than one day of data is available, renders a bar chart showing daily tokens saved over time.

<!-- dth:chunk 6b7129c7e9f875ea -->
## `Analytics`

Analytics dashboard page displaying LLM usage, savings, and repository documentation freshness over a configurable period (1–90 days). Aggregates usage data by feature, provider, model, repository, or user and allows metric selection (tokens, cost, or calls), presenting results in a stacked bar chart. Shows total calls, tokens, cost, and savings in summary statistics. Includes separate cards for savings breakdown by type, pipeline job performance metrics (p50/p95 latencies), and repository documentation freshness (last processed commit and success time).

<!-- dth:generated source="web/src/pages/Analytics.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Analytics.tsx`

Analytics dashboard displaying AI model costs, savings metrics, budget tracking, and Ask source-picker performance.

<!-- dth:chunk a0dbc7e1d631bfe8 -->
## `SiftCard`

Displays Ask's source picker performance metrics and recommendations. Shows nothing during loading, a call-to-action if no sources were picked in the period, or detailed statistics including tokens saved, net cost after judge fees, source retention rate, and a daily trend chart. The component renders metrics as a grid of stats and visualizes daily token savings via a bar chart when multiple days of data exist.

<!-- dth:chunk d9f7b7bdaa313589 -->
## `isMonthBudget`

Checks if a limit object represents a global monthly budget with a set cost threshold. Returns true only when the limit's scope is 'global', window is 'month', and max_cost_usd is defined (not null or undefined).

<!-- dth:chunk 6b7129c7e9f875ea -->
## `Analytics`

Main analytics dashboard showing usage costs, savings, and budget status. Displays daily cost trends, cost breakdown by feature, savings by method, and Ask source-picking performance. Users can select a 1–90 day period via dropdown. Fetches usage, savings, sift, and limit data; calculates per-day costs, feature breakdowns, and remaining monthly budget. Shows monthly budget status if one is configured.

<!-- dth:generated source="web/src/api/hooks.ts" — edit only inside dth:human blocks -->
# `web/src/api/hooks.ts`

This file exports React Query hooks for fetching various API data including user analytics, configuration, and resources.

<!-- dth:chunk bbc9d40c22a73d33 -->
## `SiftReport`

Reports aggregated metrics from the Sift analysis tool, tracking document filtering decisions and their financial impact. Fields record both processing statistics (answers evaluated, picked candidates, trimmed results) and cost metrics (tokens saved, judge cost, USD savings), with a daily breakdown array for time-series analysis.

<!-- dth:chunk 224f4ba166859904 -->
## `useSift`

React Query hook that fetches Sift analytics for a given date range. Accepts a `from` date parameter, constructs a query key for caching, and calls the `/analytics/sift` endpoint with the date serialized as a query string parameter, returning a `SiftReport`.

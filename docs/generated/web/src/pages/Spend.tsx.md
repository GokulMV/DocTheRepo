<!-- dth:generated source="web/src/pages/Spend.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Spend.tsx`

Page component for configuring spend limits that enforce ceilings on API calls with breach notifications.

<!-- dth:chunk ca8e286810eac12d -->
## `Spend`

Renders a page for managing spend limits that enforce hard ceilings on API usage across different scopes (global, feature, provider, repository). Users can add, edit, or remove limits with configurable windows (daily/monthly), token/cost thresholds, and breach actions (block or block-with-alert). The component fetches existing limits via `useLimits()`, tracks local edits in state, and persists changes via `api.put()` using the `useInvalidating` hook to refresh data. Converts empty string inputs to `null` for optional numeric fields. Displays a tip if no global limit is configured.

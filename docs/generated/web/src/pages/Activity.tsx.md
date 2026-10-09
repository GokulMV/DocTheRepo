<!-- dth:generated source="web/src/pages/Activity.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Activity.tsx`

Activity page component displaying system event history with filterable status and type selectors.

<!-- dth:chunk fd9be81db3dc6180 -->
## `__module__`

Constants mapping activity metadata to human-readable strings for the Activity page. `STATUSES` and `TYPES` define valid job states and task categories (prefixed with empty string for unset values). `GONE` maps resource types to descriptions for deleted entities. `ACTIONS` is an extensive lookup table converting internal action codes (like `'auth.login'`, `'repo.create'`, `'code_push:done'`) and status transitions into user-facing activity descriptions (e.g., "signed in", "repository tracked", "code read"). These are used to render the activity log with understandable labels for all possible system events.

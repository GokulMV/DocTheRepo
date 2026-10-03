<!-- dth:generated source="web/src/pages/Inbox.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Inbox.tsx`

<!-- dth:chunk 03076c959b9574dc -->
## `MarkKnown`

Creates a dialog for marking selected issues as known, generating a rule that will suppress matching events. For a single issue, it marks that specific issue; for multiple, it creates a rule matching their fingerprints and optionally services. The rule can optionally expire after a specified number of days. The dialog collects a title, reason category, and optional expiration duration, then submits via `/issues/{id}/mark-known` or `/known-issues` endpoint and invalidates the issues and known-issues caches on success.

<!-- dth:chunk 9157105e91b919c8 -->
## `Inbox`

Renders the main inbox page displaying issues with filtering, sorting, and bulk actions. Provides multiple filter inputs (search, status, severity, source, kind, service, environment, time range) and displays issues in a table with optional checkboxes for editors. When issues are selected, an editor-only "Mark as known" button appears. Shows empty state with link to add signal sources when no issues match filters. Selected issues can trigger the MarkKnown dialog.

<!-- dth:chunk f373bba27960ae29 -->
## `__module__`

Module-level constants defining available issue filters: STATUSES lists valid status values (empty string for open/non-suppressed, plus new, decoded, regressed, acknowledged, resolved, suppressed), and KINDS maps short type codes to display labels for different issue categories (errors, alerts, security findings, log matches, event-bus problems).

<!-- dth:generated source="web/src/pages/KnownIssues.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/KnownIssues.tsx`

Page component for managing known-issue suppression rules that suppress or label matching error events.

<!-- dth:chunk ea73bed6e41b8f15 -->
## `RuleDialog`

Modal form component for creating or editing a known-issue rule. Renders input fields for title, reason, action (suppress or label_only), match conditions, and description, then POSTs the rule to the API. Accepts optional initial values and an origin (source metadata) to be included with the submission. Calls `onDone` on save or cancel.

<!-- dth:chunk 8c8ee165077f52b9 -->
## `Rules`

Displays a table of known-issue suppression rules with their match criteria, hit counts, and status. If `canEdit` is true, shows buttons to create new rules, toggle enabled status, or delete rules. Renders an empty state if no rules exist, or a detailed table row per rule showing title, reason, source, ticket links, and expiration info. Integrates a `RuleDialog` component for rule creation.

<!-- dth:generated source="web/src/components/signals.tsx" — edit only inside dth:human blocks -->
# `web/src/components/signals.tsx`

<!-- dth:chunk 1d38ada04e912f09 -->
## `MatchEditor`

Form component for editing a known-issue rule's match criteria. All fields present must match together (AND logic). Accepts a `Match` object containing fingerprints, message pattern (RE2 regex), services, environments, sources, and maximum severity level. Provides text inputs for comma-separated lists and dropdowns for severity selection. Calls `onChange` with the updated `Match` object when any field changes.

<!-- dth:chunk fb27224aeeaa3127 -->
## `reasonLabel`

Formats a reason string into sentence case using the `sentence` utility function.

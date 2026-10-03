<!-- dth:generated source="web/src/components/CommandPalette.tsx" — edit only inside dth:human blocks -->
# `web/src/components/CommandPalette.tsx`

<!-- dth:chunk 7c9a33b83699dcae -->
## `Entry`

Represents a single searchable entry in the command palette, combining both navigation pages and questions. Each entry has a unique key, display label, category hint, icon, and navigation target.

<!-- dth:chunk 4bb76426f2b6b2f5 -->
## `CommandPalette`

A keyboard-driven search dialog (Ctrl/⌘+K) for navigating pages and previous questions or creating new ones. Searches across available pages filtered by user role and recent questions; Enter, arrow keys, and mouse click navigate. Clears input when closed and supports creating new questions from the typed query.

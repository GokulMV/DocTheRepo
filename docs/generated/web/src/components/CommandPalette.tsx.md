<!-- dth:generated source="web/src/components/CommandPalette.tsx" — edit only inside dth:human blocks -->
# `web/src/components/CommandPalette.tsx`

A command palette React component for quick navigation and search across pages and previous questions, with support for creating new questions.

<!-- dth:chunk 4bb76426f2b6b2f5 -->
## `CommandPalette`

A command palette component (triggered with Ctrl/⌘+K) that provides quick navigation and search. It filters navigation pages and previous questions by role and feature flags, displays matching results with keyboard navigation (arrow keys, Enter), and allows users to ask new questions by typing. Results include an "Ask" option prepended when a search query exists. The component uses memoization to recompute entries only when the query, role, threads, or feature flags change. Selection is managed with arrow keys (up/down to navigate, Enter to select), mouse hover, or direct click. The dialog clears the input when closed and navigates to the selected destination.

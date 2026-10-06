<!-- dth:generated source="web/src/components/CommandPalette.tsx" — edit only inside dth:human blocks -->
# `web/src/components/CommandPalette.tsx`

Provides a keyboard-driven command palette component for navigating pages and searching questions across the application.

<!-- dth:chunk 4bb76426f2b6b2f5 -->
## `CommandPalette`

Renders a keyboard-accessible command palette (Ctrl/⌘+K) for quick navigation and question search. It displays filterable pages from the navigation structure and recent questions from threads, with an option to create a new question from the search text. 

Accepts `open` (visibility state), `onOpenChange` (state callback), and `role` (for permission-based filtering). Filters entries by role using `atLeast()`, maintains keyboard navigation with arrow keys and Enter to select, and clears the search on close. The palette shows matching pages and up to 5-8 questions, prioritizes a "new question" action when searching, and indicates the selected entry with highlighting and an Enter icon.

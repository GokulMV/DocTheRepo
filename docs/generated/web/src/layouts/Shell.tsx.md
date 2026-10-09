<!-- dth:generated source="web/src/layouts/Shell.tsx" — edit only inside dth:human blocks -->
# `web/src/layouts/Shell.tsx`

Provides the main authenticated layout shell with sidebar navigation and content outlet for the DocTheRepo application.

<!-- dth:chunk bcf47c5a93e166bf -->
## `SideLink`

Renders a single sidebar navigation item as a link with an icon and label. When the sidebar is collapsed, the label is hidden and shown only as a tooltip/aria-label. The active state applies styling and sets `aria-current="page"`. The icon color changes based on active state and hover.

<!-- dth:chunk dee378749bd38e27 -->
## `SectionTabs`

Renders a horizontal tab navigation showing pages within the current section, filtered by user role. Returns `null` if fewer than two tabs are available. The active tab is indicated with a colored bottom border and the `aria-current="page"` attribute. Uses `navItemFor()` and `tabFor()` to locate the current section and active tab based on the URL pathname.

<!-- dth:chunk 0b016a36b05c2537 -->
## `Shell`

Renders the authenticated application shell with a collapsible sidebar and main content area. The sidebar includes role-based navigation filtered by user permissions and feature flags, a search command palette (accessible via Ctrl/⌘+K), theme switcher, and user account menu with logout. Handles API errors (401 redirects to login with return URL), shows loading spinner during auth check, and resets error boundaries on navigation. The main area renders page content via outlet with environment banner and section tabs, maintaining scroll-to-top on route changes.

<!-- dth:chunk 9f7b243cbe48f136 -->
## `__module__`

Module-level exports and constants: `NAV` is exported from the nav submodule; `THEMES` defines available color scheme options; `COLLAPSE_KEY` stores the sidebar collapsed state in localStorage; `rowBase`, `rowIdle`, and `rowActive` are shared Tailwind class definitions for sidebar link styling.

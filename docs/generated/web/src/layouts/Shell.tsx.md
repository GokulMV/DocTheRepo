<!-- dth:generated source="web/src/layouts/Shell.tsx" — edit only inside dth:human blocks -->
# `web/src/layouts/Shell.tsx`

Shell.tsx defines the authenticated app layout with a sidebar navigation component, search command palette, and page outlet.

<!-- dth:chunk bcf47c5a93e166bf -->
## `SideLink`

Renders a single sidebar navigation item as a link with an icon and label. When the sidebar is collapsed, the label is hidden and shown only as a tooltip/aria-label. The active state applies styling and sets `aria-current="page"`. The icon color changes based on active state and hover.

<!-- dth:chunk dee378749bd38e27 -->
## `SectionTabs`

Renders a horizontal tab navigation showing pages within the current section, filtered by user role. Returns `null` if fewer than two tabs are available. The active tab is indicated with a colored bottom border and the `aria-current="page"` attribute. Uses `navItemFor()` and `tabFor()` to locate the current section and active tab based on the URL pathname.

<!-- dth:chunk 0b016a36b05c2537 -->
## `Shell`

The authenticated layout component that displays a collapsible sidebar navigation filtered by user role, with a command palette and main content outlet. Handles user authentication (redirects to login if 401, shows error if API unavailable), scrolls to top on navigation, listens for Cmd/Ctrl+K to open search, manages sidebar collapse state, and provides logout functionality. The sidebar shows the user's avatar, name, and role, along with sections and a "New question" quick-action button.

<!-- dth:chunk 9f7b243cbe48f136 -->
## `__module__`

Module-level exports and constants: `NAV` is exported from the nav submodule; `THEMES` defines available color scheme options; `COLLAPSE_KEY` stores the sidebar collapsed state in localStorage; `rowBase`, `rowIdle`, and `rowActive` are shared Tailwind class definitions for sidebar link styling.

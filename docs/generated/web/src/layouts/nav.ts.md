<!-- dth:generated source="web/src/layouts/nav.ts" — edit only inside dth:human blocks -->
# `web/src/layouts/nav.ts`

Defines the navigation menu structure and item interface for the web application, including role-based access control and feature-gated visibility.

<!-- dth:chunk a84878839d02e321 -->
## `Tab`

Represents a navigable page within a section, displayed as a tab. Each tab has a route (`to`), display label, optional role-based access control (`min`), and can declare additional paths that belong to it via `also` for detail pages or legacy URLs.

<!-- dth:chunk 6eff426d4497e413 -->
## `NavItem`

Interface that defines a navigation menu item. Requires a destination URL (`to`), display label, minimum access role, and icon. Optionally includes a brief description (`blurb`) displayed under page titles, sub-pages (`tabs`), and conditional display based on feature availability (e.g., 'issues' or 'system' features reported by the Hub in `/me` response).

<!-- dth:chunk d552cc4effcc5194 -->
## `under`

Helper function that checks if a pathname matches or is under a given route. Used by `navItemFor` and `tabFor` to determine which nav item or tab is active for the current URL, supporting both exact matches and nested paths.

<!-- dth:chunk 699784fb1abbb9ac -->
## `tabFor`

tabFor finds the tab a path belongs to (/inbox/123 → Inbox).

<!-- dth:chunk 9675caf8f2bd63d9 -->
## `navItemFor`

navItemFor finds the section a path belongs to (/known-issues → Issues).

<!-- dth:chunk 20d318a4ab36e30c -->
## `__module__`

Exports the main navigation structure as `NAV`, an array of navigation items covering documentation, system architecture, issue tracking, repositories, analytics, and admin settings. Features like 'system' (multi-repository relationships) and 'issues' are opt-in and hidden unless enabled. Nested `tabs` define subsections; some tabs require elevated access levels (e.g., 'Security' tab requires 'editor' role). The `also` field in tab definitions registers additional routes that should highlight the same section.

<!-- dth:generated source="web/src/layouts/nav.ts" — edit only inside dth:human blocks -->
# `web/src/layouts/nav.ts`

Defines the navigation structure and types for the application's main menu and page organization.

<!-- dth:chunk a84878839d02e321 -->
## `Tab`

Represents a navigable page within a section, displayed as a tab. Each tab has a route (`to`), display label, optional role-based access control (`min`), and can declare additional paths that belong to it via `also` for detail pages or legacy URLs.

<!-- dth:chunk 6eff426d4497e413 -->
## `NavItem`

Represents a top-level navigation item with a route, label, icon, and optional role-based visibility. Can contain sub-tabs for organizing related pages, and optionally a feature gate that hides it unless the Hub reports support for that feature (e.g., 'issues'). The `blurb` field provides secondary description text shown below the page title.

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

Navigation structure defining the main app sections: Docs (with Architecture and Team docs tabs), Issues (Inbox and Known issues), Repositories (with Activity and Security tabs), Usage analytics, and Settings (Connections, AI models, People, and Advanced tabs with legacy URL mappings). Role gates restrict access (viewer, editor, admin), and the Issues section requires the 'issues' feature flag.

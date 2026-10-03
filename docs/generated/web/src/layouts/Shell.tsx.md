<!-- dth:generated source="web/src/layouts/Shell.tsx" — edit only inside dth:human blocks -->
# `web/src/layouts/Shell.tsx`

<!-- dth:chunk 0a418357653d4975 -->
## `usePins`

Returns the user's pinned navigation page URLs and a toggle function to add or remove pins. Persists pins to browser `localStorage` under the `PINS_KEY`, gracefully degrading to a session-only list if storage fails or contains invalid data. The returned toggle function updates both state and storage atomically.

<!-- dth:chunk bcf47c5a93e166bf -->
## `SideLink`

Renders a sidebar navigation row with an icon (and optional label if not collapsed), styled with hover states and active link highlighting. When not collapsed and `onPin` is provided, displays a pin/unpin button on hover. The icon color changes based on active state, and the label uses a tooltip in collapsed mode for accessibility.

<!-- dth:chunk 17b29691ba22ece8 -->
## `SectionLabel`

A small styled label for section headings in the sidebar, using subdued text and minimal padding.

<!-- dth:chunk 9d5e3a96a436b8f1 -->
## `Recents`

Displays the 15 most recent questions/threads with delete functionality. Returns null if no items exist. Each item shows the thread title; clicking navigates to it, and a hover-revealed trash button deletes it with confirmation, then navigates to `/ask` if the deleted thread was currently active.

<!-- dth:chunk 0b016a36b05c2537 -->
## `Shell`

The authenticated app layout component with collapsible sidebar navigation, search triggering, and main content outlet. Renders pinned and recent pages, role-filtered navigation sections, theme switcher, and user account button. Scrolls to top on route changes and handles Cmd/Ctrl+K to open search. Redirects to login with return URL on 401 errors and displays error state if user data fails to load.

<!-- dth:chunk 26d1f3829f872068 -->
## `isProduction`

isProduction: production deployments (and ones that set no name) show no banner.

<!-- dth:chunk aebd28e8f52d0a46 -->
## `EnvironmentBanner`

Displays a yellow warning banner for non-production deployments, preventing confusion between environments. Updates the document title to indicate the environment name (e.g., `[staging] DocTheRepo`). Returns null for production environments.

<!-- dth:chunk 9f7b243cbe48f136 -->
## `__module__`

Module exports and constants: re-exports `NAV`, defines theme options, and establishes localStorage keys for sidebar collapse and pinned items state. Defines shared Tailwind class strings for styling navigation rows in idle and active states across collapsed and expanded modes.

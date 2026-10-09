<!-- dth:generated source="web/src/App.tsx" — edit only inside dth:human blocks -->
# `web/src/App.tsx`

Root application component that sets up routing configuration, error handling, and suspense boundaries for the DocTheRepo application.

<!-- dth:chunk e0e0842c45116d03 -->
## `usePrefetchHeavyPages`

Prefetches the heavy Analytics component during browser idle time to improve load performance. Uses the browser's `requestIdleCallback` API if available (falling back to a 1.5-second timeout), ensuring the analytics module loads without blocking user interactions. The load errors are silently ignored.

<!-- dth:chunk 6d15624fbc8a564b -->
## `App`

The root `App` component configures the application's client-side routing structure. It wraps the entire route tree in an `ErrorBoundary` to catch runtime errors and a `Suspense` boundary with a spinner fallback for lazy-loaded code. Public routes include `/login` and `/invite/:token`. The Shell layout wraps most app pages (ask, docs, architecture, library, security, repos, inbox, known-issues, activity, analytics, account) with navigation and authentication context. Admin-protected routes (connectors, providers, spend/advanced settings, users, setup) use the `admin()` wrapper. The root path redirects to `/ask`, and an old `/palace/*` path redirects to `/architecture`. A catch-all route returns a "Page not found" message. The component also calls `usePrefetchHeavyPages()` to optimize loading of computationally expensive pages.

<!-- dth:chunk ebee488e85a9b573 -->
## `__module__`

Lazy-loaded wrapper for the Analytics page component. The actual module is loaded asynchronously via `loadAnalytics` to defer parsing and execution until needed, reducing the initial bundle size.

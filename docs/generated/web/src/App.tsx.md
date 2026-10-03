<!-- dth:generated source="web/src/App.tsx" — edit only inside dth:human blocks -->
# `web/src/App.tsx`

<!-- dth:chunk 6d15624fbc8a564b -->
## `App`

Root component that defines the application's routing structure. Sets up nested routes with authentication-based access control, wrapping all routes in error boundary and suspense fallback handling. Calls `usePrefetchHeavyPages()` to prefetch resource-intensive pages. Public routes include login and invite flows; authenticated routes are nested under `Shell` layout and include document browsing, palace (entity viewer), architecture analysis, library, security, activity/analytics, and account management. Admin-only routes (connectors, providers, spend, users, setup, settings) are wrapped with the `admin()` helper. Returns 404 for unmatched paths.

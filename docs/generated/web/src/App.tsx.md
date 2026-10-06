<!-- dth:generated source="web/src/App.tsx" — edit only inside dth:human blocks -->
# `web/src/App.tsx`

Root application component that configures routing, error handling, and suspense boundaries for the DocTheRepo web application.

<!-- dth:chunk e0e0842c45116d03 -->
## `usePrefetchHeavyPages`

Prefetches the heavy Analytics component during browser idle time to improve load performance. Uses the browser's `requestIdleCallback` API if available (falling back to a 1.5-second timeout), ensuring the analytics module loads without blocking user interactions. The load errors are silently ignored.

<!-- dth:chunk 6d15624fbc8a564b -->
## `App`

Root application component that sets up routing and page structure. Prefetches heavy resources on mount, wraps the route tree in error boundary and suspense for resilience, and defines all application routes organized under an authenticated `Shell` layout, with public login/invite routes, and role-gated admin routes. Routes include documentation, ask/chat, architecture, library, security, inbox, activity, and analytics features.

<!-- dth:chunk ebee488e85a9b573 -->
## `__module__`

Lazy-loaded wrapper for the Analytics page component. The actual module is loaded asynchronously via `loadAnalytics` to defer parsing and execution until needed, reducing the initial bundle size.

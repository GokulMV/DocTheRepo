<!-- dth:generated source="web/src/layouts/nav.ts" — edit only inside dth:human blocks -->
# `web/src/layouts/nav.ts`

<!-- dth:chunk 20d318a4ab36e30c -->
## `__module__`

Defines the main navigation structure as a hierarchical array of sections (Knowledge, Signals, Operations, Administration), each containing navigation items with route paths, display labels, minimum required roles ('viewer', 'editor', 'admin', or 'owner'), and associated Lucide icons. The Security item includes an optional description blurb. Also exports a `PRIMARY` array listing three routes ('/docs', '/architecture', '/inbox') that are prioritized in the UI, likely for prominent placement or quick access.

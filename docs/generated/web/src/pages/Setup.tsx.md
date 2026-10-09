<!-- dth:generated source="web/src/pages/Setup.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Setup.tsx`

Provides the initial setup and health-check page guiding users through connecting GitHub, adding an AI model, selecting repositories, and generating documentation.

<!-- dth:chunk 298d64c9b0f86b88 -->
## `Setup`

A first-run checklist component displaying five required setup steps (sign-in, GitHub connection, model selection, repository selection, and docs generation) plus optional enhancements. Each step shows its completion status, explains why it's needed, what resources are required, and links to its configuration page. The component reads live state from multiple hooks—connectors, providers, routes, repositories, and documentation—so it functions as a real-time health check. Required steps are ordered with the next incomplete step highlighted; optional steps are hidden behind a toggle. For docs v2, it queries each repository to determine which have documents.

In docs v2 mode, it fetches document counts per repository to calculate `hasDocs`. The sign-in step only appears for account owners. The component persists the password-only sign-in choice to localStorage (with a try-catch for private browsing mode) and uses `readFlag()` to restore it on mount.

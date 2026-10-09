<!-- dth:generated source="web/src/pages/Docs.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Docs.tsx`

Provides the main documentation page entry point, with feature-flag-based routing between legacy and v2 documentation interfaces.

<!-- dth:chunk b382b464ec9fd2a1 -->
## `NoDocs`

Renders a card explaining where documentation comes from and guiding users on next steps. If repositories are already tracked, displays the current doc generation status (queued jobs, running jobs, errors) and offers a button to generate docs manually if the user is an admin. If no repositories are tracked, shows a three-step onboarding flow: connect GitHub/GitLab, track repositories, and push code or import docs.

Fetches job data for running, queued, and failed doc-generation jobs; displays detailed progress for active jobs and error details for the most recent failure. On manual generation, posts to `/repos/{id}/generate-docs` for each tracked repository and updates queued list, catching and displaying errors.

<!-- dth:chunk 80c1b944ff972a81 -->
## `Docs`

Default export that conditionally routes between documentation interfaces based on user feature flags. Returns the new `RepoDocsPage` if the user has the `docs_v2` feature enabled; otherwise falls back to the `LegacyDocs` component.

<!-- dth:chunk 59662d442dcb1d8a -->
## `LegacyDocs`

Displays the legacy documentation browser. Uses `useTree()` to fetch the documentation tree structure and `useDocNode()` to load a specific node based on the URL parameter. Renders a `DocsBrowser` if tree data exists, otherwise shows a loading spinner, error message, or empty state. Navigation updates the URL when a user selects a node.

<!-- dth:generated source="web/src/pages/Docs.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Docs.tsx`

This file contains the Docs page, which displays documentation for tracked repositories and guides users through setting up documentation generation.

<!-- dth:chunk b382b464ec9fd2a1 -->
## `NoDocs`

Renders a card explaining where documentation comes from and guiding users on next steps. If repositories are already tracked, displays the current doc generation status (queued jobs, running jobs, errors) and offers a button to generate docs manually if the user is an admin. If no repositories are tracked, shows a three-step onboarding flow: connect GitHub/GitLab, track repositories, and push code or import docs.

Fetches job data for running, queued, and failed doc-generation jobs; displays detailed progress for active jobs and error details for the most recent failure. On manual generation, posts to `/repos/{id}/generate-docs` for each tracked repository and updates queued list, catching and displaying errors.

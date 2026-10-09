<!-- dth:generated source="web/src/pages/Repos.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Repos.tsx`

Page component for managing tracked repositories, displaying their documentation status and providing admin controls for doc generation and import operations.

<!-- dth:chunk 62d2a596c20a47af -->
## `Repos`

The Repos page displays a list of tracked repositories and allows admins to manage them. It fetches repositories via `useRepos()` and the current user via `useMe()`, then renders a table showing each repository's details including full name, branch, docs path, landing mode, and last processed commit. Admins can generate docs, import existing markdown docs, or edit settings for each repo; editors can perform dry runs. State tracks which repo is being edited or dry-run tested, newly added repos, and operation status messages. The `useInvalidating` hook wraps API calls to generate or import docs, displaying user-friendly notifications about operation progress. Access to actions is role-gated: admins see all actions, editors see only dry run, and non-privileged users see no actions.

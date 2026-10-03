<!-- dth:generated source="web/src/pages/Activity.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Activity.tsx`

<!-- dth:chunk c813ec43a2cf8ea7 -->
## `JobDetail`

Modal dialog displaying a job's details including error message, JSON result/payload, and retry options. Admins can retry failed/aborted/spend-blocked jobs; spend-blocked jobs require explicit confirmation to retry above the ceiling and trigger an audit. The dialog title shows the job type and status; shows queued job ID on successful retry.

<!-- dth:chunk 729b838966b1b1cb -->
## `actionLabel`

Converts an action string to a human-readable label. Actions with a colon (like "code_push:done") are split into a job type and status; others (like "user.invite") have dots replaced with spaces. Both are transformed to sentence case with the status portion lowercased.

<!-- dth:chunk 24914ffbb881bd00 -->
## `useNames`

Returns a function that resolves entity references in activity feed text to human-readable names. Builds a map from various API queries (repos, connectors, providers, users) and singleton entries (auth settings), then replaces inline references like "repo:abc-123" with names like "my-repo/path". Handles removed entities by showing fallback text ("a removed repository"), model routes via feature labels, and spend limits. Requires admin role to view connector and provider data.

<!-- dth:chunk 9abb9a0c1dc35450 -->
## `Activity`

Admin dashboard page showing two side-by-side sections: an activity feed filtered by type (all/jobs/docs PRs/admin actions) with action badges and entity names, and a jobs table filtered by type and status with attempt counts and real-time progress bars. Clicking a job row opens a detail modal. The feed resolves references to names and applies predefined labels; unmatched actions fall back to generated labels.

<!-- dth:chunk fd9be81db3dc6180 -->
## `__module__`

Constants defining the available job statuses, job types, and human-readable labels for actions and removed entities. The ACTIONS map provides explicit translations for known audit and job events, used as the primary lookup in the feed before falling back to generated labels.

<!-- dth:generated source="web/src/pages/Connectors.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Connectors.tsx`

<!-- dth:chunk ba6df4dcf2ce9748 -->
## `ConnectGitHub`

Component starting the recommended one-click GitHub connection flow. Generates a start URL with optional organization and Enterprise Server base URL parameters, then redirects to GitHub's OAuth flow where GitHub creates a private app for the Hub. Includes expandable fields for organization and base URL.

<!-- dth:chunk c025e52ad4c66758 -->
## `ExistingResult`

Type representing the result of an API call to connect an existing GitHub App to a connector. Contains the connector ID, app slug, installation status, an optional GitHub URL for installing the app, and an optional account name when the app is installed on multiple accounts.

<!-- dth:chunk 54960b6699dbd619 -->
## `ExistingGitHubApp`

Component allowing reuse of an existing GitHub App by providing its App ID and a newly generated private key. Queries the Hub to find where the app is installed, handles selection when installed on multiple accounts, and guides users to install the app on GitHub if needed. Shows a collapsible form hidden by default.

<!-- dth:chunk 4f456b2322b9c323 -->
## `PickRepos`

Component listing repositories accessible to a newly connected git host and allowing selection of repositories to track. Fetches available repositories, displays untracked ones with select-all/none buttons, tracks selected repositories one by one, and shows progress. Calls `onDone` after successful tracking or when user chooses to skip.

<!-- dth:chunk c633fd2cfe165525 -->
## `AddGit`

Component to connect a GitHub or GitLab host, supporting token authentication (fine-grained token for GitHub or personal token for GitLab), GitHub App authentication (app ID and installation ID), optional group limiting for GitLab, and webhook/polling/both modes. Generates a webhook secret, seals credentials and secret before sending, and calls `onCreated` with connector ID, type, and secret.

<!-- dth:chunk ccbef18cffc2c6b8 -->
## `AddSignal`

Component to add a signal source (Sentry, Datadog, PagerDuty, Wiz, Splunk, and cloud log platforms). Accepts source type, name, mode (webhook/polling/both when applicable), optional configuration fields, and optional credentials. Generates webhook secret only if the source type supports webhooks, seals credentials if provided, and calls `onCreated` with connector details including webhook path.

<!-- dth:chunk 8c2743b37d6cf81b -->
## `AddKnowledge`

Component to add a knowledge source (Confluence, Jira, or Notion). Takes source type, name, configuration fields (workspace, cloud ID, etc. depending on source), and API token. Seals token before sending, creates connector with mode set to 'poll', and calls `onCreated` with the source type.

<!-- dth:chunk fb0750ac2bd030ad -->
## `Connectors`

Main connectors page displaying all connected sources (git hosts, signal sources, knowledge sources) in a table with health, last sync time, and mode. Handles GitHub connection success/error via search params, guides through webhook setup for new git connectors and signal sources, supports enabling/disabling/removing connectors with appropriate warnings for GitHub Apps. Shows sync and test results.

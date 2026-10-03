<!-- dth:generated source="web/src/pages/Repos.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Repos.tsx`

Page for viewing and managing repositories tracked for documentation generation, with controls to add, configure, and trigger doc generation jobs.

<!-- dth:chunk e88464a085b2ce5e -->
## `AddRepo`

Dialog component that allows admins to track a new repository. Collects the git connector and repository full name (owner/name format), then posts to the `/repos` endpoint. Filters available connectors to GitHub and GitLab only. Uses the first available connector by default if none is selected. Disables submission if no connectors exist or while the request is pending. Calls `onAdded` callback with the repository name and optional webhook URL upon successful submission.

<!-- dth:chunk bf1ddf1399cf0ce5 -->
## `EditRepo`

Dialog component for editing repository settings. Allows configuration of tracked branch (defaulting to the repository's default branch), docs path, push mode (with conditional approver field for `pr_with_approver` mode), PR conflict strategy, service name, and enabled status. Submits changes via a PATCH request to `/repos/{id}`. Closes dialog after successful save, with unsaved changes discarded if the dialog is cancelled.

<!-- dth:chunk 62d2a596c20a47af -->
## `Repos`

Main repositories page showing a list of all tracked repositories with their branch, docs path, and landing mode. Displays the last processed commit SHA and provides action buttons: "Dry run" for editors, and "Generate docs", "Settings", and "Import docs" for admins. Includes inline forms for tracking new repositories and displays status messages for recently added repositories and in-progress operations like imports and full regenerations. Only admins can add repositories and manage settings.

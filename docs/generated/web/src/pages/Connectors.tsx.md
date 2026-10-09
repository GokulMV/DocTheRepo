<!-- dth:generated source="web/src/pages/Connectors.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Connectors.tsx`

Page component for displaying and managing integrations that feed data into the Hub from code repositories, documentation, error tracking, and MCP tools.

<!-- dth:chunk c633fd2cfe165525 -->
## `AddGit`

Modal dialog for manually connecting GitHub or GitLab repositories. Supports GitHub token/app authentication, GitLab with optional group filtering, and configurable webhook vs. polling delivery modes. Seals credentials and webhook secrets before sending to the API, then invokes `onCreated` with the connector ID and secret for webhook setup display.

<!-- dth:chunk fb0750ac2bd030ad -->
## `Connectors`

Main page for managing all integrations: code repositories (GitHub/GitLab), team documentation (Confluence, Jira, Notion), error/alert tools (Sentry, Datadog, etc.), and MCP integrations for live lookups. Handles connector lifecycle (create, delete, toggle enable/disable, sync), displays GitHub OAuth flow results, webhook setup instructions, and knowledge base sync status. Organizes connectors into four groups with add buttons and action rows (sync, test, disable/remove), filtering popular alert tools by default with a toggle to show all.

<!-- dth:chunk 8ccb0226705f9f4d -->
## `Tile`

Renders a clickable tile button for connecting a service, displaying a label and subtitle. The button is styled consistently across the connectors interface and includes an aria-label for accessibility.

<!-- dth:chunk 79d6c11cdefdc1b9 -->
## `Group`

Wraps grouped connector sections with a title, optional badge, description text, and child content. Organizes the UI into distinct categories (Code, Team documents, Errors and alerts) with consistent spacing and styling.

<!-- dth:chunk 9d19d72958c169b1 -->
## `ConnectorTable`

Displays a table of connected tools showing their name, health status, last sync time, and action buttons. Each connector row shows enabled/disabled state, type and link mode, sealed credentials indicator, and any recent errors. The `actions` callback provides custom buttons per row.

<!-- dth:chunk 32e87635532626f1 -->
## `__module__`

Module-level constants: `remoteDone` maps GitHub App lifecycle actions (suspended, resumed, uninstalled) to user-facing messages; `POPULAR` lists the most common alert tool types for filtering display; `tileClass` defines shared Tailwind styles for connector tile buttons with dark mode support.

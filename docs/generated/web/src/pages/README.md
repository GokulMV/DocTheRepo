<!-- dth:generated index — edit only inside dth:human blocks -->
# `web/src/pages`

- [`Activity.tsx`](Activity.tsx.md) — Activity page component displaying system event history with filterable status and type selectors.
- [`Analytics.tsx`](Analytics.tsx.md) — Analytics dashboard displaying AI model costs, savings metrics, budget tracking, and Ask source-picker performance.
- [`Architecture.tsx`](Architecture.tsx.md) — Displays repository architecture graphs with extracted components, dependencies, and custom diagrams, with permissions-based scanning and node detail inspect...
- [`Ask.tsx`](Ask.tsx.md) — The Ask.tsx page implements an AI-powered question-answering interface with message threads, streaming responses, citations, and user feedback collection.
- [`ConnectTools.tsx`](ConnectTools.tsx.md) — Exports components and utilities for connecting external alert tools and knowledge bases to the Hub via dialogs that guide users through setup.
- [`Connectors.tsx`](Connectors.tsx.md) — Page component for displaying and managing integrations that feed data into the Hub from code repositories, documentation, error tracking, and MCP tools.
- [`Docs.tsx`](Docs.tsx.md) — Provides the main documentation page entry point, with feature-flag-based routing between legacy and v2 documentation interfaces.
- [`Library.tsx`](Library.tsx.md) — Component for browsing and managing documentation organized into shelves, supporting multiple document sources with role-based upload and creation permissions.
- [`McpConnections.tsx`](McpConnections.tsx.md) — Page component for managing MCP (Model Context Protocol) server connections in the Ask Hub, including browsing available servers, adding new connections, and...
- [`ModelSetup.tsx`](ModelSetup.tsx.md) — Configuration page for selecting LLM providers and models for different features, and setting a monthly budget cap on model API costs.
- [`Providers.tsx`](Providers.tsx.md) — The Providers page for managing AI model provider accounts, their configurations, and feature-to-model routing in a Hub application.
- [`RepoDocs.tsx`](RepoDocs.tsx.md) — Renders the repository documentation viewer and editor, displaying auto-generated documents with confidence scoring, citations linked to source code, and adm...
- [`Repos.tsx`](Repos.tsx.md) — Page component for managing tracked repositories, displaying their documentation status and providing admin controls for doc generation and import operations.
- [`SettingsGroups.tsx`](SettingsGroups.tsx.md) — Settings page sections for managing Hub users, sign-in, spending, and configuration.
- [`Setup.tsx`](Setup.tsx.md) — Provides the initial setup and health-check page guiding users through connecting GitHub, adding an AI model, selecting repositories, and generating document...
- [`System.tsx`](System.tsx.md) — React component that visualizes how repositories in a codebase interconnect through APIs, events, and packages, with generated documentation and an interacti...
- [`Uploads.tsx`](Uploads.tsx.md) — Page component for viewing a single uploaded document with deletion capability.
- [`mcpCatalog.ts`](mcpCatalog.ts.md) — Defines the MCP (Model Context Protocol) service catalog structure and exports pre-configured integrations with cloud providers, monitoring tools, and collab...

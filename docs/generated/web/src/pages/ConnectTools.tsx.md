<!-- dth:generated source="web/src/pages/ConnectTools.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/ConnectTools.tsx`

Exports components and utilities for connecting external alert tools and knowledge bases to the Hub via dialogs that guide users through setup.

<!-- dth:chunk d6f72d6bf42972a7 -->
## `randomSecret`

Generates a cryptographically random 48-character hex string for webhook secrets. Uses `Uint8Array` and `crypto.getRandomValues()` to ensure unpredictability.

<!-- dth:chunk 748d1f8504befa61 -->
## `Guide`

Describes how an alert tool proves delivery authenticity and what steps the user must follow. `secret` indicates the proof method: `'link'` embeds the key in the webhook URL, `'theirs'` means the tool generates its own secret (pasted back to the Hub), and `'header'` means the key travels in a custom HTTP header. Optional `keyField` and `theirName` provide tool-specific naming for these fields.

<!-- dth:chunk 67586226de901342 -->
## `b`

Shorthand helper that wraps a string in bold HTML tags for use in instructional text.

<!-- dth:chunk 50d8c3a59a5a52d7 -->
## `useUniqueName`

Returns a function that generates unique connector names by appending a number if needed. Queries existing connectors and returns the base name if available, otherwise returns "base 2", "base 3", etc., with case-insensitive comparison.

<!-- dth:chunk f27b6fb8adc9def3 -->
## `More`

Collapsible details element with a label and animated chevron icon. Renders children inside a bordered container that expands on click. Defaults to "More options" as the label text.

<!-- dth:chunk f048a2693f5bc59a -->
## `ConfigFields`

Renders form fields from a source spec's config schema, filtering by required or optional. Formats config keys as human-readable labels (snake_case to Title Case) and binds input values to the `config` object via the `set` callback.

<!-- dth:chunk 332ab50b30698e91 -->
## `ConnectAlerts`

Dialog to connect an alert tool (Sentry, PagerDuty, Datadog, etc.) to the Hub. Manages credentials, webhook setup, and LLM opt-out. For webhook-based tools, generates a random secret and displays step-by-step instructions with the webhook link; for poll-based tools, accepts credentials directly and marks connection complete. Supports dual webhook+poll mode when available.

<!-- dth:chunk 70114b58eec6826b -->
## `ConnectDocs`

Dialog to connect a knowledge source (Confluence, Jira, Notion) to the Hub. Collects site/workspace configuration fields and an API token (encrypted via `seal`), then creates a poll connector. Shows required fields first, optional fields in a collapsible "More options" section.

<!-- dth:chunk d37906d43ca688c8 -->
## `__module__`

Lookup tables defining step-by-step instructions and field names for each alert tool integration (Sentry, PagerDuty, Datadog, Grafana, etc.) and token-retrieval instructions for knowledge sources (Confluence, Jira, Notion). The `GUIDES` object keys match alert tool types and specify the authentication flow; `TOKEN_STEPS` describes how to obtain API tokens for each knowledge source.

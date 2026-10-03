<!-- dth:generated source="web/src/pages/Account.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Account.tsx`

<!-- dth:chunk abe7bddc63aa05a7 -->
## `CopyBlock`

Renders a code block with a copy-to-clipboard button. Displays the text in a monospace dark-themed box with a button in the top-right that copies text to the clipboard on click and shows a temporary checkmark confirmation. The button uses an aria-label for accessibility based on the `label` prop.

<!-- dth:chunk 404b72ed5ff6f6a4 -->
## `Snippets`

Shows tabbed usage snippets for an API token across three integration methods: dth CLI, Claude Code (MCP), and curl. Each tab displays a pre-filled command snippet with the token and server URL. Includes a note for Claude Code users pointing to documentation for opencode and Cursor setup.

<!-- dth:chunk c917d6b996d3a699 -->
## `Account`

Renders the account page with two main sections: user email/role header, and an access token manager. Users can create new tokens (specifying purpose and expiration), view created tokens in a table (showing creation/last-use/expiration dates), and revoke tokens. After creating a token, displays it once with usage snippets for CLI, Claude Code, and curl. Uses React state for form inputs and token creation/revocation mutations via the API.

<!-- dth:chunk 41a20a9fbd9ca2ff -->
## `__module__`

Defines three usage scenarios for access tokens: terminal CLI commands, AI coding tools via MCP, and API calls from scripts/CI. Each entry has an icon, title, and description displayed as use-case cards.

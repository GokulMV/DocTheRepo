-- A GitHub App's OAuth client (client ID and sealed client secret), so an admin can sign in to the GitHub
-- MCP server on GitHub's own page with the Hub's App instead of pasting a token. Empty for other connectors.
ALTER TABLE connectors ADD COLUMN oauth_client_id text NOT NULL DEFAULT '';
ALTER TABLE connectors ADD COLUMN oauth_client_secret_ct bytea; -- envelope-encrypted, bound to the row

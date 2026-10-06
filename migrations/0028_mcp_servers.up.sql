-- MCP connections: other products' MCP servers the Hub calls (read-only by default) while answering.
CREATE TABLE mcp_servers (
  id                uuid PRIMARY KEY,
  name              text NOT NULL UNIQUE,
  url               text NOT NULL,
  catalog_key       text NOT NULL DEFAULT '',
  auth              text NOT NULL DEFAULT 'none' CHECK (auth IN ('none', 'bearer', 'header', 'oauth', 'aws', 'google')),
  -- Non-secret settings: header name, extra headers, AWS region/service, OAuth scope.
  config            jsonb NOT NULL DEFAULT '{}',
  -- The token, header value, AWS keys or Google service-account JSON (sealed; empty means none or ambient).
  secret_ciphertext bytea,
  -- OAuth client and tokens (sealed JSON).
  oauth_ciphertext  bytea,
  min_role          text NOT NULL DEFAULT 'editor' CHECK (min_role IN ('viewer', 'editor', 'admin', 'owner')),
  enabled           boolean NOT NULL DEFAULT true,
  -- The tools the server offers, as last listed, and the admin's on/off choices by tool name.
  tools             jsonb NOT NULL DEFAULT '[]',
  tool_choices      jsonb NOT NULL DEFAULT '{}',
  status            text NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'ok', 'needs_sign_in', 'error')),
  last_error        text NOT NULL DEFAULT '',
  checked_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);

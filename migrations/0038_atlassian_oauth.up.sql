-- The Atlassian OAuth 2.0 (3LO) app the operator registers once in the Atlassian developer console, so admins
-- can connect Confluence and Jira Cloud by approving on Atlassian's consent page. One row; the client secret is
-- sealed by the Box (covered by rotate-key). Connectors' own tokens stay in connectors.creds_ciphertext.
CREATE TABLE atlassian_oauth_app (
  id                   smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  client_id            text NOT NULL,
  client_secret_ct     bytea NOT NULL,
  updated_at           timestamptz NOT NULL DEFAULT now()
);

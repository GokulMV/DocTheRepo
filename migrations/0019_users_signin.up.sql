-- Invite links: an admin adds a person, who sets their own password from a one-time link (also used to
-- reset a password). Only a hash of the token is stored.
CREATE TABLE user_invites (
    token_hash  bytea PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_invites_user_idx ON user_invites (user_id);

-- Sign-in settings set in the UI (one row). Empty values fall back to the deployment's config file and
-- environment, so existing setups keep working.
CREATE TABLE auth_settings (
    id                      smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    password_enabled        boolean,            -- NULL: the deployment default (on in local mode)
    oidc_provider           text NOT NULL DEFAULT '',  -- google | microsoft | okta | keycloak | other (a label)
    oidc_issuer             text NOT NULL DEFAULT '',
    oidc_client_id          text NOT NULL DEFAULT '',
    oidc_secret_ciphertext  bytea,              -- sealed by the Box
    oidc_allowed_domains    text[] NOT NULL DEFAULT '{}',
    oidc_groups_claim       text NOT NULL DEFAULT '',
    updated_at              timestamptz NOT NULL DEFAULT now()
);

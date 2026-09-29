-- Identity, access control, and audit (plan § 6.1).
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TYPE user_role AS ENUM ('viewer', 'editor', 'admin', 'owner');
CREATE TYPE repo_access_level AS ENUM ('read', 'admin');

CREATE TABLE users (
    id             uuid PRIMARY KEY,
    email          citext NOT NULL UNIQUE,
    name           text NOT NULL DEFAULT '',
    oidc_subject   text UNIQUE,
    password_hash  text,               -- local-mode owner only (argon2id); NULL for OIDC users
    role           user_role NOT NULL DEFAULT 'viewer',
    disabled       boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_login_at  timestamptz
);

CREATE TABLE sessions (
    id_hash     bytea PRIMARY KEY,     -- sha256 of the random session id; the raw id lives only in the cookie
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token  text NOT NULL,
    expires_at  timestamptz NOT NULL,  -- absolute expiry
    idle_until  timestamptz NOT NULL,  -- sliding idle expiry
    created_at  timestamptz NOT NULL DEFAULT now(),
    ip          text NOT NULL DEFAULT '',
    user_agent  text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expiry_idx ON sessions (expires_at);

CREATE TABLE api_tokens (
    id            uuid PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          text NOT NULL,
    token_hash    bytea NOT NULL UNIQUE,
    scopes        text[] NOT NULL DEFAULT '{}',
    expires_at    timestamptz,
    last_used_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id);

CREATE TABLE groups (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,  -- matches the IdP groups claim value
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id  uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user_idx ON group_members (user_id);

CREATE TABLE audit_log (
    id             uuid PRIMARY KEY,
    at             timestamptz NOT NULL DEFAULT now(),
    actor_user_id  uuid REFERENCES users(id) ON DELETE SET NULL,
    action         text NOT NULL,
    target_type    text NOT NULL DEFAULT '',
    target_id      text NOT NULL DEFAULT '',
    details        jsonb NOT NULL DEFAULT '{}',
    ip             text NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_user_id, at DESC);

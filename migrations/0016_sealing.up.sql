-- The Hub's hybrid (X25519 + ML-KEM-768) sealing keys. Browsers and the CLI encrypt secrets to the active
-- key before sending them; the private half is stored sealed by the key-encryption key (local file or KMS).
CREATE TABLE seal_keys (
    id          text PRIMARY KEY,
    x25519_pub  bytea NOT NULL,
    mlkem_pub   bytea NOT NULL,
    private_ct  bytea NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    retired_at  timestamptz
);
-- At most one active key.
CREATE UNIQUE INDEX seal_keys_one_active ON seal_keys ((true)) WHERE retired_at IS NULL;

-- A short, non-secret hint for write-only secrets (the last 4 characters of long keys) and when they were set.
ALTER TABLE llm_providers ADD COLUMN key_hint text NOT NULL DEFAULT '', ADD COLUMN key_set_at timestamptz;
ALTER TABLE connectors ADD COLUMN creds_hint text NOT NULL DEFAULT '', ADD COLUMN creds_set_at timestamptz;

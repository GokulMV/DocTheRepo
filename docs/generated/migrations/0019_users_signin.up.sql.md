<!-- dth:generated source="migrations/0019_users_signin.up.sql" — edit only inside dth:human blocks -->
# `migrations/0019_users_signin.up.sql`

<!-- dth:chunk 3eb1fe1f4ecbdd16 -->
## `migrations/0019_users_signin.up.sql`

This migration creates two tables for authentication infrastructure. The `user_invites` table stores one-time invitation/password-reset tokens as hashes, linked to users by reference, with expiration and usage tracking; it includes an index on `user_id` for efficient lookups. The `auth_settings` table is a singleton configuration table (restricted to one row by primary key constraint) that allows UI-configurable authentication parameters like password enablement and OpenID Connect settings (provider, issuer, client ID, encrypted secret, allowed domains, and groups claim); NULL values fall back to deployment defaults to maintain backward compatibility.

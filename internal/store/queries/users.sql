-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (id, email, name, oidc_subject, password_hash, role)
VALUES (sqlc.arg(id), sqlc.arg(email), sqlc.arg(name), sqlc.narg(oidc_subject), sqlc.narg(password_hash), sqlc.arg(role))
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = sqlc.arg(email);

-- name: GetUserBySubject :one
SELECT * FROM users WHERE oidc_subject = sqlc.arg(subject);

-- name: LinkUserSubject :exec
UPDATE users SET oidc_subject = sqlc.arg(subject), name = CASE WHEN sqlc.arg(name)::text = '' THEN name ELSE sqlc.arg(name) END
WHERE id = sqlc.arg(id);

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = now(), name = CASE WHEN sqlc.arg(name)::text = '' THEN name ELSE sqlc.arg(name) END WHERE id = sqlc.arg(id);

-- name: SetUserPassword :exec
UPDATE users SET password_hash = sqlc.arg(password_hash) WHERE id = sqlc.arg(id);

-- name: ListUsers :many
SELECT * FROM users WHERE email > sqlc.arg(after) ORDER BY email LIMIT sqlc.arg(lim);

-- name: UpdateUser :one
UPDATE users SET role = coalesce(sqlc.narg(role), role), disabled = coalesce(sqlc.narg(disabled), disabled),
                 name = coalesce(sqlc.narg(name), name)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = sqlc.arg(id);

-- name: CreateInvite :exec
INSERT INTO user_invites (token_hash, user_id, created_by, expires_at)
VALUES (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.narg(created_by), sqlc.arg(expires_at));

-- name: GetInvite :one
SELECT i.user_id, i.expires_at, i.used_at, u.email, u.name, u.disabled
FROM user_invites i JOIN users u ON u.id = i.user_id
WHERE i.token_hash = sqlc.arg(token_hash);

-- name: UseInvites :exec
-- Accepting one link retires every open link for that person.
UPDATE user_invites SET used_at = now() WHERE user_id = sqlc.arg(user_id) AND used_at IS NULL;

-- name: PendingInviteUsers :many
SELECT DISTINCT user_id FROM user_invites WHERE used_at IS NULL AND expires_at > now();

-- name: GetAuthSettings :one
SELECT * FROM auth_settings WHERE id = 1;

-- name: UpsertAuthSettings :exec
INSERT INTO auth_settings (id, password_enabled, oidc_provider, oidc_issuer, oidc_client_id, oidc_secret_ciphertext,
                           oidc_allowed_domains, oidc_groups_claim, updated_at)
VALUES (1, sqlc.narg(password_enabled), sqlc.arg(oidc_provider), sqlc.arg(oidc_issuer), sqlc.arg(oidc_client_id),
        sqlc.narg(oidc_secret_ciphertext), sqlc.arg(oidc_allowed_domains), sqlc.arg(oidc_groups_claim), now())
ON CONFLICT (id) DO UPDATE SET
    password_enabled = EXCLUDED.password_enabled, oidc_provider = EXCLUDED.oidc_provider, oidc_issuer = EXCLUDED.oidc_issuer,
    oidc_client_id = EXCLUDED.oidc_client_id, oidc_secret_ciphertext = EXCLUDED.oidc_secret_ciphertext,
    oidc_allowed_domains = EXCLUDED.oidc_allowed_domains, oidc_groups_claim = EXCLUDED.oidc_groups_claim, updated_at = now();

-- name: CountOwners :one
SELECT count(*) FROM users WHERE role = 'owner' AND NOT disabled;

-- name: CreateSession :exec
INSERT INTO sessions (id_hash, user_id, csrf_token, expires_at, idle_until, ip, user_agent)
VALUES (sqlc.arg(id_hash), sqlc.arg(user_id), sqlc.arg(csrf_token), sqlc.arg(expires_at), sqlc.arg(idle_until), sqlc.arg(ip), sqlc.arg(user_agent));

-- name: GetSession :one
SELECT s.id_hash, s.csrf_token, s.expires_at, s.idle_until, u.id AS user_id, u.email, u.name, u.role, u.disabled
FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id_hash = sqlc.arg(id_hash);

-- name: SlideSession :exec
UPDATE sessions SET idle_until = sqlc.arg(idle_until) WHERE id_hash = sqlc.arg(id_hash);

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id_hash = sqlc.arg(id_hash);

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = sqlc.arg(user_id);

-- name: GCSessions :execrows
DELETE FROM sessions WHERE expires_at < now() OR idle_until < now();

-- name: CreateToken :exec
INSERT INTO api_tokens (id, user_id, name, token_hash, scopes, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(token_hash), sqlc.arg(scopes), sqlc.narg(expires_at));

-- name: GetTokenByHash :one
SELECT t.id, t.expires_at, t.scopes, u.id AS user_id, u.email, u.name, u.role, u.disabled
FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = sqlc.arg(token_hash);

-- name: TouchToken :exec
UPDATE api_tokens SET last_used_at = now() WHERE id = sqlc.arg(id) AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: ListTokens :many
SELECT id, name, scopes, expires_at, last_used_at, created_at FROM api_tokens WHERE user_id = sqlc.arg(user_id) ORDER BY created_at DESC;

-- name: DeleteToken :execrows
DELETE FROM api_tokens WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: UpsertGroup :one
INSERT INTO groups (id, name) VALUES (sqlc.arg(id), sqlc.arg(name))
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id;

-- name: ClearUserGroups :exec
DELETE FROM group_members WHERE user_id = sqlc.arg(user_id);

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id) VALUES (sqlc.arg(group_id), sqlc.arg(user_id)) ON CONFLICT DO NOTHING;

-- name: UserRepoIDs :many
-- Repositories a user can read through direct grants or IdP groups.
SELECT ra.repo_id FROM repo_access ra WHERE ra.user_id = sqlc.arg(uid)::uuid
UNION
SELECT gra.repo_id FROM group_repo_access gra JOIN group_members gm ON gm.group_id = gra.group_id WHERE gm.user_id = sqlc.arg(uid)::uuid;

-- name: DirectRepoAccess :many
-- A user's direct grants (not those through IdP groups), for the access editor.
SELECT repo_id, level::text AS level FROM repo_access WHERE user_id = sqlc.arg(uid)::uuid ORDER BY repo_id;

-- name: ClearRepoAccess :exec
DELETE FROM repo_access WHERE user_id = sqlc.arg(user_id);

-- name: GrantRepoAccess :exec
INSERT INTO repo_access (user_id, repo_id, level) VALUES (sqlc.arg(user_id), sqlc.arg(repo_id), sqlc.arg(level))
ON CONFLICT (user_id, repo_id) DO UPDATE SET level = EXCLUDED.level;

-- name: InsertAudit :exec
INSERT INTO audit_log (id, actor_user_id, action, target_type, target_id, details, ip)
VALUES (sqlc.arg(id), sqlc.narg(actor_user_id), sqlc.arg(action), sqlc.arg(target_type), sqlc.arg(target_id), sqlc.arg(details), sqlc.arg(ip));

-- name: ListAudit :many
SELECT * FROM audit_log
WHERE (sqlc.narg(actor)::uuid IS NULL OR actor_user_id = sqlc.narg(actor))
  AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action))
  AND at >= sqlc.arg(since)
  AND (sqlc.narg(before)::timestamptz IS NULL OR at < sqlc.narg(before))
ORDER BY at DESC LIMIT sqlc.arg(lim);

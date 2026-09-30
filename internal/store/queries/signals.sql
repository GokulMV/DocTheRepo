-- name: CreateKnownIssue :exec
INSERT INTO known_issues (id, title, description, explanation, source_text, jira_key, reason, match, action, enabled, expires_at,
                          source, confluence_page_id, owner_user_id, ticket_url)
VALUES (sqlc.arg(id), sqlc.arg(title), sqlc.arg(description), sqlc.arg(explanation), sqlc.arg(source_text), sqlc.narg(jira_key),
        sqlc.arg(reason), sqlc.arg(match), sqlc.arg(action), sqlc.arg(enabled), sqlc.narg(expires_at), sqlc.arg(source),
        sqlc.narg(confluence_page_id), sqlc.narg(owner_user_id), sqlc.arg(ticket_url));

-- name: UpdateKnownIssue :execrows
UPDATE known_issues SET title = sqlc.arg(title), description = sqlc.arg(description), reason = sqlc.arg(reason), match = sqlc.arg(match),
       action = sqlc.arg(action), enabled = sqlc.arg(enabled), expires_at = sqlc.narg(expires_at), ticket_url = sqlc.arg(ticket_url),
       updated_at = now()
WHERE id = sqlc.arg(id);

-- name: GetKnownIssue :one
SELECT * FROM known_issues WHERE id = sqlc.arg(id);

-- name: ListKnownIssues :many
SELECT * FROM known_issues ORDER BY created_at DESC, id LIMIT sqlc.arg(lim);

-- name: DeleteKnownIssue :execrows
DELETE FROM known_issues WHERE id = sqlc.arg(id);

-- name: ActiveKnownIssues :many
-- Rules the matcher compiles: enabled and not expired (expiry is also checked per event).
SELECT id, match, action, expires_at FROM known_issues
WHERE enabled AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at, id;

-- name: ListServiceMap :many
SELECT service_name, repo_id, path_prefix, source_patterns FROM service_map ORDER BY service_name, repo_id, path_prefix;

-- name: GetIssueByFingerprint :one
SELECT * FROM issues WHERE fingerprint = sqlc.arg(fingerprint);

-- name: IssueSamples :many
SELECT * FROM event_samples WHERE issue_id = sqlc.arg(issue_id) ORDER BY occurred_at DESC LIMIT sqlc.arg(lim);

-- name: IssueMinuteCounts :many
SELECT minute, count, suppressed_count FROM issue_counts_minutely
WHERE issue_id = sqlc.arg(issue_id) AND minute >= sqlc.arg(since) ORDER BY minute;

-- name: IssueHourCounts :many
SELECT hour, count, suppressed_count FROM issue_counts_hourly
WHERE issue_id = sqlc.arg(issue_id) AND hour >= sqlc.arg(since) ORDER BY hour;

-- name: DecodeEstimate :one
-- Average cost of the last 100 decodes (the estimate a suppression saves).
SELECT COALESCE(avg(tokens), 0)::bigint AS tokens, COALESCE(avg(cost_usd), 0)::float8 AS cost_usd
FROM (SELECT tokens, cost_usd FROM decodes ORDER BY created_at DESC LIMIT 100) d;

-- name: DeleteMinuteCountsBefore :execrows
DELETE FROM issue_counts_minutely WHERE minute < sqlc.arg(before);

-- name: DeleteHourCountsBefore :execrows
DELETE FROM issue_counts_hourly WHERE hour < sqlc.arg(before);

-- name: ResolveIssuesByFingerprint :execrows
UPDATE issues SET status = 'resolved', resolved_at = now(), updated_at = now()
WHERE fingerprint = ANY(sqlc.arg(fingerprints)::text[]) AND status NOT IN ('resolved', 'suppressed');

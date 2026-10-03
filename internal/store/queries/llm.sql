-- name: InsertUsageEvent :exec
INSERT INTO usage_events (id, at, feature, provider_id, provider_kind, model, input_tokens, output_tokens, cost_usd, latency_ms,
                          cached, estimated, repo_id, user_id, job_id, issue_id, outcome, cache_read_tokens, cache_write_tokens)
VALUES (sqlc.arg(id), sqlc.arg(at), sqlc.arg(feature), sqlc.narg(provider_id), sqlc.arg(provider_kind), sqlc.arg(model),
        sqlc.arg(input_tokens), sqlc.arg(output_tokens), sqlc.arg(cost_usd), sqlc.arg(latency_ms),
        sqlc.arg(cached), sqlc.arg(estimated), sqlc.narg(repo_id), sqlc.narg(user_id), sqlc.narg(job_id),
        sqlc.narg(issue_id), sqlc.arg(outcome), sqlc.arg(cache_read_tokens), sqlc.arg(cache_write_tokens));

-- name: SpentSince :one
-- Sums consumption for a spend limit's window.
SELECT coalesce(sum(input_tokens + output_tokens), 0)::bigint AS tokens,
       coalesce(sum(cost_usd), 0)::float8                    AS cost_usd
FROM usage_events
WHERE at >= sqlc.arg(since)
  AND (sqlc.narg(feature)::llm_feature IS NULL OR feature = sqlc.narg(feature))
  AND (sqlc.narg(provider_id)::uuid IS NULL OR provider_id = sqlc.narg(provider_id))
  AND (sqlc.narg(repo_id)::uuid IS NULL OR repo_id = sqlc.narg(repo_id));

-- name: GetRoute :one
SELECT r.feature, r.provider_id, p.kind AS provider_kind, p.enabled AS provider_enabled, p.redact_pii AS provider_redact_pii,
       r.model, r.max_output_tokens, r.context_token_budget, r.temperature, r.effort, r.fallback_provider_id, r.fallback_model,
       fp.kind AS fallback_kind, fp.enabled AS fallback_enabled, fp.redact_pii AS fallback_redact_pii
FROM model_routes r
JOIN llm_providers p ON p.id = r.provider_id
LEFT JOIN llm_providers fp ON fp.id = r.fallback_provider_id
WHERE r.feature = sqlc.arg(feature);

-- name: ListRoutes :many
SELECT r.feature, r.provider_id, r.model, r.max_output_tokens, r.context_token_budget, r.temperature, r.effort,
       r.fallback_provider_id, r.fallback_model, r.updated_at
FROM model_routes r ORDER BY r.feature;

-- name: UpsertRoute :exec
INSERT INTO model_routes (feature, provider_id, model, max_output_tokens, context_token_budget, temperature, effort,
                          fallback_provider_id, fallback_model, updated_at)
VALUES (sqlc.arg(feature), sqlc.arg(provider_id), sqlc.arg(model), sqlc.arg(max_output_tokens),
        sqlc.arg(context_token_budget), sqlc.narg(temperature), sqlc.arg(effort), sqlc.narg(fallback_provider_id),
        sqlc.arg(fallback_model), now())
ON CONFLICT (feature) DO UPDATE SET provider_id = EXCLUDED.provider_id, model = EXCLUDED.model,
    max_output_tokens = EXCLUDED.max_output_tokens, context_token_budget = EXCLUDED.context_token_budget,
    temperature = EXCLUDED.temperature, effort = EXCLUDED.effort, fallback_provider_id = EXCLUDED.fallback_provider_id,
    fallback_model = EXCLUDED.fallback_model, updated_at = now();

-- name: GetProvider :one
SELECT * FROM llm_providers WHERE id = sqlc.arg(id);

-- name: ListProviders :many
SELECT * FROM llm_providers ORDER BY name;

-- name: InsertProvider :exec
INSERT INTO llm_providers (id, kind, name, base_url, key_ciphertext, extra, redact_pii, enabled)
VALUES (sqlc.arg(id), sqlc.arg(kind), sqlc.arg(name), sqlc.arg(base_url), sqlc.narg(key_ciphertext), sqlc.arg(extra),
        sqlc.arg(redact_pii), sqlc.arg(enabled));

-- name: UpdateProvider :execrows
UPDATE llm_providers
SET name = sqlc.arg(name), base_url = sqlc.arg(base_url), extra = sqlc.arg(extra), redact_pii = sqlc.arg(redact_pii),
    enabled = sqlc.arg(enabled), key_ciphertext = coalesce(sqlc.narg(key_ciphertext), key_ciphertext), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: DeleteProvider :execrows
DELETE FROM llm_providers WHERE id = sqlc.arg(id);

-- name: ListSpendLimits :many
SELECT * FROM spend_limits ORDER BY scope, scope_key, time_window;

-- name: ListCostTable :many
SELECT * FROM cost_table ORDER BY provider_kind, model;

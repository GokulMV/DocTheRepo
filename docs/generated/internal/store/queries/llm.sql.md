<!-- dth:generated source="internal/store/queries/llm.sql" — edit only inside dth:human blocks -->
# `internal/store/queries/llm.sql`

<!-- dth:chunk 81431aadbf9174f5 -->
## `internal/store/queries/llm.sql`

This SQL file contains sqlc-generated queries for managing LLM provider integrations, usage tracking, and spend limits. It includes queries to record usage events with token and cost metrics, query accumulated usage within time windows, manage model routing configurations (primary and fallback providers per feature), configure LLM providers with encryption-protected API keys, and retrieve spend limits and cost tables. Notable patterns: `sqlc.narg()` is used for nullable columns; `UpsertRoute` uses `ON CONFLICT` to update existing routes by feature; `UpdateProvider` uses `coalesce()` to conditionally update encrypted keys; `SpentSince` filters usage by optional feature, provider, and repository with null-checking logic to handle unrestricted queries.

<!-- dth:generated source="migrations/0021_cache_pricing.up.sql" — edit only inside dth:human blocks -->
# `migrations/0021_cache_pricing.up.sql`

<!-- dth:chunk fc010de9c3d25e6a -->
## `migrations/0021_cache_pricing.up.sql`

This migration extends the usage tracking and pricing system to handle prompt cache tokens separately from regular input tokens. It adds `cache_read_tokens` and `cache_write_tokens` columns to `usage_events` to track how much cached input was read from or written to the cache, while `input_tokens` continues to report the total (with token ceilings counting all tokens). An `investigated` boolean flag is added to `qa_messages` to indicate whether an answer required the agent to look further. The `cost_table` gains nullable `cache_read_per_mtok_usd` and `cache_write_per_mtok_usd` columns for provider-specific cache pricing; when NULL, cache tokens default to plain input pricing. For Anthropic's seeded pricing, cache reads are set to 0.1× input price and 5-minute cache writes to 1.25× input price, reflecting Anthropic's billing model where cache reads cost significantly less but writes cost more than regular input.

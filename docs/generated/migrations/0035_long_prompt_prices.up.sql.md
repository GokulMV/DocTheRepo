<!-- dth:generated source="migrations/0035_long_prompt_prices.up.sql" — edit only inside dth:human blocks -->
# `migrations/0035_long_prompt_prices.up.sql`

Database migration that adds tiered pricing columns to support higher rates for long prompts on models like Claude Haiku 5.5.

<!-- dth:chunk 7ac361860dd20545 -->
## `migrations/0035_long_prompt_prices.up.sql`

Migration that adds support for tiered pricing based on prompt length. It introduces five new columns to the `cost_table`: `long_prompt_threshold_tokens` (the token count threshold), and four corresponding long-prompt pricing columns (`long_input_per_mtok_usd`, `long_output_per_mtok_usd`, `long_cache_read_per_mtok_usd`, `long_cache_write_per_mtok_usd`). When a prompt's total tokens (input + cache read + cache write) exceeds the threshold, the long-form rates apply instead of base rates; NULL thresholds disable tiered pricing. The migration also backfills Claude Haiku 5.5 with cache pricing (reads at 0.1x, 5-minute writes at 1.25x the input rate) and configures 100K-token threshold pricing at 5x multiplier for all token types.

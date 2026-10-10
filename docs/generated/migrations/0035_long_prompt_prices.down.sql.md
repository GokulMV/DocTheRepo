<!-- dth:generated source="migrations/0035_long_prompt_prices.down.sql" — edit only inside dth:human blocks -->
# `migrations/0035_long_prompt_prices.down.sql`

Rollback migration that removes long-prompt pricing columns from the cost_table.

<!-- dth:chunk 02157ef1bfeba657 -->
## `migrations/0035_long_prompt_prices.down.sql`

Rollback migration that removes five columns added for long-prompt pricing from the `cost_table` table: cache write/read rates, output rate, input rate, and the prompt token threshold that determines when long-prompt pricing applies. Used to revert database schema changes if migration 0035 needs to be undone.

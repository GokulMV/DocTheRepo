<!-- dth:generated source="migrations/0034_seed_haiku_5_5_price.down.sql" — edit only inside dth:human blocks -->
# `migrations/0034_seed_haiku_5_5_price.down.sql`

Database migration to remove seeded pricing data for the Anthropic Claude Haiku 5.5 model during rollback.

<!-- dth:chunk e51d7984c1b4fa1c -->
## `migrations/0034_seed_haiku_5_5_price.down.sql`

Rollback migration that removes the seeded pricing data for the Claude Haiku 5.5 model from the cost table. This down migration deletes rows matching the specific provider (anthropic), model (claude-haiku-5-5), and source (seeded) to undo the corresponding up migration that populated these pricing records.

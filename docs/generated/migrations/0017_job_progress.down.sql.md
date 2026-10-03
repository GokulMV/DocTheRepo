<!-- dth:generated source="migrations/0017_job_progress.down.sql" — edit only inside dth:human blocks -->
# `migrations/0017_job_progress.down.sql`

<!-- dth:chunk ed0078c26875922c -->
## `migrations/0017_job_progress.down.sql`

Reverts the schema change from the corresponding up migration by removing the `progress` column from the `jobs` table, used when rolling back database migrations.

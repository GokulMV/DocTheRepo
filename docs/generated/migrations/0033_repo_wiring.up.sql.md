<!-- dth:generated source="migrations/0033_repo_wiring.up.sql" — edit only inside dth:human blocks -->
# `migrations/0033_repo_wiring.up.sql`

Database migration that creates the repo_wiring table to track inter-repository dependencies discovered from configuration and CI files.

<!-- dth:chunk 35afc463b38111ba -->
## `migrations/0033_repo_wiring.up.sql`

Creates the `repo_wiring` table to store cross-repository dependencies and integrations extracted from configuration and CI files. Each row represents a connection of a specific kind (hosted service, called service, published/consumed container image, or CI reference) with its location in source files. The table uses cascade delete to clean up wiring records when a repository is removed, and maintains a composite primary key on (repo_id, kind, value, path) to prevent duplicates. The secondary index on (kind, value) enables efficient lookups across repositories for a given dependency type and target.

<!-- dth:generated source="migrations/0030_docs_phase2.down.sql" — edit only inside dth:human blocks -->
# `migrations/0030_docs_phase2.down.sql`

SQL downward migration that reverses phase 2 of documentation schema changes by removing an index, deleting null rows, restoring constraints, and dropping a column.

<!-- dth:chunk 00819a182d7b156d -->
## `migrations/0030_docs_phase2.down.sql`

This downward migration reverses the changes from `0030_docs_phase2.up.sql`. It removes the `repo_docs_system_key` index, deletes any orphaned rows in `repo_docs` where `repo_id` is null, restores the `repo_id` column to be non-nullable in the `repo_docs` table, and drops the `confidence` column from the `qa_messages` table.

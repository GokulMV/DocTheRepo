<!-- dth:generated source="migrations/0024_notion_uploads.down.sql" — edit only inside dth:human blocks -->
# `migrations/0024_notion_uploads.down.sql`

<!-- dth:chunk f3803ca171c7c8aa -->
## `migrations/0024_notion_uploads.down.sql`

Down migration that reverts changes from the up migration for notion and upload source types. Deletes all rows from knowledge_docs, chunks, and connectors tables where the source type is 'notion' or 'upload', then removes the `uploaded_by` and `body` columns from knowledge_docs, and updates the source check constraint to only allow 'confluence' and 'jira' as valid source types. PostgreSQL enums are left in place since the database does not support dropping enum values.

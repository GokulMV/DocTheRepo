<!-- dth:generated source="migrations/0024_notion_uploads.up.sql" — edit only inside dth:human blocks -->
# `migrations/0024_notion_uploads.up.sql`

<!-- dth:chunk 934d46a12b148871 -->
## `migrations/0024_notion_uploads.up.sql`

Migration adds support for Notion pages and user-uploaded documents as knowledge sources.

Adds `'notion'` and `'upload'` as new enum values to `chunk_source`, `connector_type`, and `shelf_item_type` to distinguish these source types. Notion represents external Notion workspace pages, while `upload` is an internal connector for documents uploaded to the Library.

Updates the `knowledge_docs_source_check` constraint to validate that the source column contains only valid values: 'confluence', 'jira', 'notion', or 'upload'.

Adds two columns to `knowledge_docs`: `body` (text, required, defaults to empty string) stores the Markdown content for display in the Hub, and `uploaded_by` (uuid, nullable foreign key to users) tracks which user uploaded the document, with deletions setting it to null.

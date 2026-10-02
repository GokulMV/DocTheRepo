DROP INDEX IF EXISTS known_issues_confluence_page_uidx;
DROP INDEX IF EXISTS known_issues_jira_key_uidx;
ALTER TABLE known_issues DROP COLUMN IF EXISTS label_managed, DROP COLUMN IF EXISTS upstream_note, DROP COLUMN IF EXISTS upstream_status;
DELETE FROM shelf_items WHERE item_type IN ('confluence_page', 'jira_issue');
DROP TABLE IF EXISTS knowledge_docs;
-- shelf_item_type keeps 'jira_issue': PostgreSQL cannot drop an enum value.

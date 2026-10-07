DROP INDEX IF EXISTS repo_docs_system_key;
DELETE FROM repo_docs WHERE repo_id IS NULL;
ALTER TABLE repo_docs ALTER COLUMN repo_id SET NOT NULL;
ALTER TABLE qa_messages DROP COLUMN IF EXISTS confidence;

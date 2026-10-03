-- Enum values stay (PostgreSQL cannot drop them); the rows that use them go.
DELETE FROM knowledge_docs WHERE source::text IN ('notion', 'upload');
DELETE FROM chunks WHERE source::text IN ('notion', 'upload');
DELETE FROM connectors WHERE type::text IN ('notion', 'upload');
ALTER TABLE knowledge_docs DROP COLUMN uploaded_by;
ALTER TABLE knowledge_docs DROP COLUMN body;
ALTER TABLE knowledge_docs DROP CONSTRAINT IF EXISTS knowledge_docs_source_check;
ALTER TABLE knowledge_docs ADD CONSTRAINT knowledge_docs_source_check CHECK (source::text IN ('confluence', 'jira'));

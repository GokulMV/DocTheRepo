-- More team knowledge: Notion pages (a knowledge connector, like Confluence) and documents uploaded to the
-- Library by people. Both are shared sources: every viewer may read them.
ALTER TYPE chunk_source ADD VALUE IF NOT EXISTS 'notion';
ALTER TYPE chunk_source ADD VALUE IF NOT EXISTS 'upload';
ALTER TYPE connector_type ADD VALUE IF NOT EXISTS 'notion';
ALTER TYPE connector_type ADD VALUE IF NOT EXISTS 'upload';        -- internal: holds uploaded documents
ALTER TYPE shelf_item_type ADD VALUE IF NOT EXISTS 'knowledge_doc'; -- a Notion page or an uploaded document

ALTER TABLE knowledge_docs DROP CONSTRAINT IF EXISTS knowledge_docs_source_check;
ALTER TABLE knowledge_docs ADD CONSTRAINT knowledge_docs_source_check
    CHECK (source::text IN ('confluence', 'jira', 'notion', 'upload'));
-- The Markdown of an uploaded document, so the Hub can show it (synced pages link to their own site).
ALTER TABLE knowledge_docs ADD COLUMN body text NOT NULL DEFAULT '';
ALTER TABLE knowledge_docs ADD COLUMN uploaded_by uuid REFERENCES users(id) ON DELETE SET NULL;

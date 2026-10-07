-- Answer confidence on Ask messages, and repository-wide documents (the System architecture) stored with
-- the per-repository ones, without a repository.
ALTER TABLE qa_messages ADD COLUMN confidence jsonb;
ALTER TABLE repo_docs ALTER COLUMN repo_id DROP NOT NULL;
CREATE UNIQUE INDEX repo_docs_system_key ON repo_docs (doc_type, doc_key) WHERE repo_id IS NULL;

-- What the checks found in a document's first draft when it had to be repaired (for tuning the prompts).
ALTER TABLE repo_docs ADD COLUMN draft_problems jsonb NOT NULL DEFAULT '[]';

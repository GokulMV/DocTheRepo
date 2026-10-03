-- How Ask's source picker trimmed each answer's sources (rag.SiftSummary), shown on the Ask page.
ALTER TABLE qa_messages ADD COLUMN sift jsonb;

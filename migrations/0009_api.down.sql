DROP INDEX IF EXISTS jobs_updated_idx;
DROP INDEX IF EXISTS usage_events_repo_at_idx;
DROP INDEX IF EXISTS entities_kind_idx;
ALTER TABLE doc_nodes DROP COLUMN IF EXISTS commit_sha, DROP COLUMN IF EXISTS content;

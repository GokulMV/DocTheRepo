-- Columns and indexes the M1 API reads (plan § 7.4, § 7.7).
ALTER TABLE doc_nodes
    ADD COLUMN content text NOT NULL DEFAULT '',     -- file nodes: the doc as last landed (no git round trip)
    ADD COLUMN commit_sha text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS entities_kind_idx ON entities (kind, name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS usage_events_repo_at_idx ON usage_events (repo_id, at);
CREATE INDEX IF NOT EXISTS jobs_updated_idx ON jobs (updated_at DESC) WHERE status NOT IN ('queued', 'processing');

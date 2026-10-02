-- Confluence & Jira sync (plan § 8.10, § 8.16): synced documents, Library placement, and known-issue import.

ALTER TYPE shelf_item_type ADD VALUE IF NOT EXISTS 'jira_issue';

-- One row per synced Confluence page or Jira issue. Chunks live in chunks (repo_id NULL, path = path); the
-- Palace entity is (kind confluence_page|jira_issue, key external_id).
CREATE TABLE knowledge_docs (
    id                  uuid PRIMARY KEY,
    connector_id        uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    source              chunk_source NOT NULL CHECK (source IN ('confluence', 'jira')),
    external_id         text NOT NULL,                -- page ID or issue key
    space               text NOT NULL,                -- space or project key
    title               text NOT NULL,
    url                 text NOT NULL DEFAULT '',
    labels              text[] NOT NULL DEFAULT '{}',
    status              text NOT NULL DEFAULT '',
    done                boolean NOT NULL DEFAULT false,
    path                text NOT NULL,                -- chunk path: <source>/<space>/<external_id>
    summary             text NOT NULL DEFAULT '',
    content_hash        text NOT NULL,
    upstream_updated_at timestamptz,
    synced_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, external_id)
);
CREATE INDEX knowledge_docs_connector_idx ON knowledge_docs (connector_id, space);
CREATE INDEX knowledge_docs_labels_idx ON knowledge_docs USING gin (labels);

-- Imported rules remember the upstream state: a Jira issue moving to Done (or its label being removed)
-- flips the rule to label_only with a note, so a fixed bug's errors are no longer hidden.
ALTER TABLE known_issues
    ADD COLUMN upstream_status text NOT NULL DEFAULT '',
    ADD COLUMN upstream_note   text NOT NULL DEFAULT '',
    ADD COLUMN label_managed   boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX known_issues_jira_key_uidx ON known_issues (jira_key) WHERE source = 'jira' AND jira_key IS NOT NULL;
CREATE UNIQUE INDEX known_issues_confluence_page_uidx ON known_issues (confluence_page_id) WHERE source = 'confluence' AND confluence_page_id IS NOT NULL;

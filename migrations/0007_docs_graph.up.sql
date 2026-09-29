-- Docs Tree, Palace graph, Library, docs PRs, rename audit, and reindex state (plan § 6.3, § 8.17, § 8.18).

ALTER TABLE repos
    ADD COLUMN pr_conflict_strategy text NOT NULL DEFAULT 'auto_rebase'
        CHECK (pr_conflict_strategy IN ('auto_rebase', 'requeue', 'leave_open')),
    ADD COLUMN pr_stale_after interval NOT NULL DEFAULT '72 hours',
    ADD CONSTRAINT repos_on_reject_check
        CHECK (on_reject IN ('fallback_pr_auto_merge', 'fallback_pr_with_approver', 'fail_job'));

-- Reindex: a pending version is dual-written while the rebuild runs, then swapped in atomically.
ALTER TABLE embedding_lock
    ADD COLUMN pending_version integer,
    ADD COLUMN pending_provider_kind llm_provider_kind,
    ADD COLUMN pending_model text,
    ADD COLUMN pending_dimensions integer CHECK (pending_dimensions IS NULL OR pending_dimensions > 0);

CREATE TYPE doc_node_kind AS ENUM ('repo', 'dir', 'file', 'section');

CREATE TABLE doc_nodes (
    id          text PRIMARY KEY,               -- stable: sha256(repo_id::path::chunk_id)[:16]
    repo_id     uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    parent_id   text REFERENCES doc_nodes(id) ON DELETE CASCADE,
    kind        doc_node_kind NOT NULL,
    path        text NOT NULL,                  -- doc path under docs_path (section: the file's path)
    title       text NOT NULL,
    summary     text NOT NULL DEFAULT '',
    chunk_id    text,
    order_key   text NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX doc_nodes_parent_idx ON doc_nodes (parent_id, order_key);
CREATE INDEX doc_nodes_repo_path_idx ON doc_nodes (repo_id, path);

CREATE TABLE entities (
    id          uuid PRIMARY KEY,
    kind        text NOT NULL,
    key         text NOT NULL,
    name        text NOT NULL,
    repo_id     uuid REFERENCES repos(id) ON DELETE CASCADE,
    attrs       jsonb NOT NULL DEFAULT '{}',
    first_seen  timestamptz NOT NULL DEFAULT now(),
    last_seen   timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    UNIQUE (kind, key)
);
CREATE INDEX entities_attrs_idx ON entities USING gin (attrs);
CREATE INDEX entities_repo_idx ON entities (repo_id) WHERE deleted_at IS NULL;
CREATE INDEX entities_name_idx ON entities (lower(name)) WHERE deleted_at IS NULL;

CREATE TABLE edges (
    src_id      uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    kind        text NOT NULL,
    dst_id      uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    weight      real NOT NULL DEFAULT 1,
    evidence    jsonb NOT NULL DEFAULT '{}',
    -- source_path is the file whose extraction produced the edge, so a file's contribution can be replaced.
    repo_id     uuid REFERENCES repos(id) ON DELETE CASCADE,
    source_path text NOT NULL DEFAULT '',
    last_seen   timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    PRIMARY KEY (src_id, kind, dst_id, source_path)
);
CREATE INDEX edges_dst_idx ON edges (dst_id, kind) WHERE deleted_at IS NULL;
CREATE INDEX edges_source_idx ON edges (repo_id, source_path) WHERE deleted_at IS NULL;

CREATE TABLE library_shelves (
    id           uuid PRIMARY KEY,
    slug         text NOT NULL UNIQUE,
    title        text NOT NULL,
    description  text NOT NULL DEFAULT '',
    rule         jsonb NOT NULL DEFAULT '[]',
    curated      boolean NOT NULL DEFAULT false,
    order_key    integer NOT NULL DEFAULT 0
);

CREATE TYPE shelf_item_type AS ENUM ('doc_node', 'entity', 'confluence_page', 'known_issue');

CREATE TABLE shelf_items (
    shelf_id   uuid NOT NULL REFERENCES library_shelves(id) ON DELETE CASCADE,
    item_type  shelf_item_type NOT NULL,
    item_id    text NOT NULL,
    pinned     boolean NOT NULL DEFAULT false,
    note       text NOT NULL DEFAULT '',
    PRIMARY KEY (shelf_id, item_type, item_id)
);
CREATE INDEX shelf_items_item_idx ON shelf_items (item_type, item_id);

CREATE TABLE manifest_renames (
    repo_id     uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    old_path    text NOT NULL,
    new_path    text NOT NULL,
    commit_sha  text NOT NULL,
    at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX manifest_renames_repo_idx ON manifest_renames (repo_id, at DESC);

CREATE TABLE prs (
    repo_id       uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    number        integer NOT NULL,
    url           text NOT NULL,
    branch        text NOT NULL,
    base          text NOT NULL,
    job_id        uuid REFERENCES jobs(id) ON DELETE SET NULL,
    mode          push_mode NOT NULL,
    -- open | awaiting_review | merged | closed | superseded | stale | requeued | needs_human
    state         text NOT NULL,
    chunk_ids     text[] NOT NULL DEFAULT '{}',
    source_paths  text[] NOT NULL DEFAULT '{}',
    approver      text NOT NULL DEFAULT '',
    note          text NOT NULL DEFAULT '',
    opened_at     timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, number)
);
CREATE INDEX prs_state_idx ON prs (state) WHERE state IN ('open', 'awaiting_review');

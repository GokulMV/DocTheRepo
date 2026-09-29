-- Chunk manifest and search text (plan § 6.3). Embeddings live in a separate, versioned table created by
-- the vector adapter once the embedding dimension is locked (plan § 8.18), so this migration never needs
-- to know the dimension.
CREATE TYPE chunk_source AS ENUM ('code', 'generated_doc', 'imported_doc', 'confluence', 'jira', 'issue_decode');

CREATE TABLE chunks (
    chunk_id      text PRIMARY KEY,                -- sha256(scope::path::symbol)[:16]
    repo_id       uuid REFERENCES repos(id) ON DELETE CASCADE,
    scope         text NOT NULL,                   -- repo full name, confluence:<space>, jira:<project>, issue
    source        chunk_source NOT NULL,
    path          text NOT NULL,
    symbol        text NOT NULL,
    language      text NOT NULL DEFAULT '',
    content       text NOT NULL,
    content_hash  text NOT NULL,
    signature     text NOT NULL DEFAULT '',
    commit_sha    text NOT NULL DEFAULT '',
    url           text NOT NULL DEFAULT '',
    tsv           tsvector GENERATED ALWAYS AS (
                      setweight(to_tsvector('simple', coalesce(symbol, '')), 'A') ||
                      setweight(to_tsvector('simple', coalesce(path, '')), 'B') ||
                      setweight(to_tsvector('english', coalesce(content, '')), 'C')
                  ) STORED,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);
CREATE INDEX chunks_tsv_idx ON chunks USING gin (tsv);
CREATE INDEX chunks_repo_path_idx ON chunks (repo_id, path) WHERE deleted_at IS NULL;
CREATE INDEX chunks_scope_idx ON chunks (scope, source) WHERE deleted_at IS NULL;
CREATE INDEX chunks_gc_idx ON chunks (deleted_at) WHERE deleted_at IS NOT NULL;

-- Monotonic index version; bumped on every chunk write so the answer cache invalidates cheaply.
CREATE TABLE index_version (
    id       boolean PRIMARY KEY DEFAULT true CHECK (id),
    version  bigint NOT NULL DEFAULT 0
);
INSERT INTO index_version (id, version) VALUES (true, 0);

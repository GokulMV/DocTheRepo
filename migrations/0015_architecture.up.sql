-- Architecture diagrams authored in a repository (archify HTML), synced so the Architecture tab can show them.
CREATE TABLE architecture_diagrams (
    id          uuid PRIMARY KEY,
    repo_id     uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    path        text NOT NULL,
    title       text NOT NULL,
    generator   text NOT NULL DEFAULT '',
    html        text NOT NULL,
    commit_sha  text NOT NULL DEFAULT '',
    size_bytes  integer NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repo_id, path)
);

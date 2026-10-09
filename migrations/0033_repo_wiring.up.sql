-- How each repository is wired to others, read from its configuration and CI files: hosts it serves,
-- hosts it calls, images it publishes or runs, repositories its pipelines reference. Replaced on every
-- docs run; the System links match them across repositories.
CREATE TABLE repo_wiring (
  repo_id uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  kind    text NOT NULL CHECK (kind IN ('host', 'calls', 'image_pub', 'image_use', 'ci_ref')),
  value   text NOT NULL,
  note    text NOT NULL DEFAULT '',
  path    text NOT NULL,
  line    int NOT NULL DEFAULT 0,
  PRIMARY KEY (repo_id, kind, value, path)
);
CREATE INDEX repo_wiring_value ON repo_wiring (kind, value);

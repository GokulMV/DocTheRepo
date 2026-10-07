-- Docs v2: readable documents per repository (overview, architecture, module guides and the other types),
-- the file cards they are written from, and their confidence.
CREATE TABLE repo_docs (
  id           uuid PRIMARY KEY,
  repo_id      uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  doc_type     text NOT NULL,
  doc_key      text NOT NULL,
  title        text NOT NULL,
  grp          text NOT NULL DEFAULT '',
  ord          int NOT NULL DEFAULT 0,
  at_a_glance  text NOT NULL DEFAULT '',
  sections     jsonb NOT NULL DEFAULT '[]',
  gaps         jsonb NOT NULL DEFAULT '[]',
  confidence   real NOT NULL DEFAULT 0,
  why          jsonb NOT NULL DEFAULT '[]',
  calibrated   boolean NOT NULL DEFAULT false,
  inputs_hash  text NOT NULL DEFAULT '',
  file_hashes  jsonb NOT NULL DEFAULT '{}',
  changed      real NOT NULL DEFAULT 0,
  source_sha   text NOT NULL DEFAULT '',
  model        text NOT NULL DEFAULT '',
  tokens_in    bigint NOT NULL DEFAULT 0,
  tokens_out   bigint NOT NULL DEFAULT 0,
  cost_usd     double precision NOT NULL DEFAULT 0,
  status       text NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'failed')),
  error        text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (repo_id, doc_type, doc_key)
);

CREATE TABLE file_cards (
  repo_id    uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  path       text NOT NULL,
  shape      text NOT NULL,
  card       jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (repo_id, path)
);

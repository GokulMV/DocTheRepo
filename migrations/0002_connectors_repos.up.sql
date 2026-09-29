-- Connectors, repositories, repo ACL, LLM providers and routing (plan § 6.2).
CREATE TYPE connector_type AS ENUM (
    'github', 'gitlab',
    'cloudwatch', 'firehose', 'gcp', 'pubsub',
    'datadog', 'grafana', 'alertmanager', 'sentry', 'pagerduty', 'opsgenie',
    'wiz', 'splunk', 'generic',
    'kafka', 'sqs', 'sns', 'eventbridge', 'kinesis', 'pubsub_bus', 'rabbitmq',
    'confluence', 'jira'
);
CREATE TYPE connector_mode AS ENUM ('webhook', 'poll', 'both');
CREATE TYPE health_state AS ENUM ('ok', 'degraded', 'failing', 'unknown');

CREATE TABLE connectors (
    id                   uuid PRIMARY KEY,
    type                 connector_type NOT NULL,
    name                 text NOT NULL UNIQUE,
    config               jsonb NOT NULL DEFAULT '{}',   -- non-secret settings only
    creds_ciphertext     bytea,                         -- envelope-encrypted credentials
    webhook_secret_hash  bytea,                         -- sha256 of the shared webhook secret, where applicable
    webhook_secret_ct    bytea,                         -- encrypted secret, needed to compute HMACs
    mode                 connector_mode NOT NULL DEFAULT 'webhook',
    poll_interval        interval NOT NULL DEFAULT '60 seconds',
    enabled              boolean NOT NULL DEFAULT true,
    health               health_state NOT NULL DEFAULT 'unknown',
    last_error           text NOT NULL DEFAULT '',
    last_sync_at         timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connectors_type_idx ON connectors (type) WHERE enabled;

CREATE TABLE connector_cursors (
    connector_id  uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    stream        text NOT NULL,
    cursor        text NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connector_id, stream)
);

CREATE TYPE push_mode AS ENUM ('direct', 'pr_auto_merge', 'pr_with_approver');

CREATE TABLE repos (
    id                  uuid PRIMARY KEY,
    connector_id        uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    full_name           text NOT NULL,
    default_branch      text NOT NULL DEFAULT 'main',
    tracked_branch      text NOT NULL DEFAULT '',   -- empty = default_branch
    docs_path           text NOT NULL DEFAULT 'docs/generated/',
    push_mode           push_mode NOT NULL DEFAULT 'pr_auto_merge',
    on_reject           text NOT NULL DEFAULT 'fallback_pr_auto_merge',
    approver            text NOT NULL DEFAULT '',
    last_processed_sha  text NOT NULL DEFAULT '',
    service_name        text NOT NULL DEFAULT '',
    owners              text[] NOT NULL DEFAULT '{}',
    enabled             boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (connector_id, full_name)
);

CREATE TABLE repo_access (
    user_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repo_id  uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    level    repo_access_level NOT NULL DEFAULT 'read',
    PRIMARY KEY (user_id, repo_id)
);
CREATE INDEX repo_access_repo_idx ON repo_access (repo_id);

CREATE TABLE group_repo_access (
    group_id  uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    repo_id   uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    level     repo_access_level NOT NULL DEFAULT 'read',
    PRIMARY KEY (group_id, repo_id)
);

CREATE TABLE service_map (
    service_name     text NOT NULL,
    repo_id          uuid NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    path_prefix      text NOT NULL DEFAULT '',
    source_patterns  jsonb NOT NULL DEFAULT '{}',  -- log groups, GCP resource labels, k8s namespaces, consumer groups
    PRIMARY KEY (service_name, repo_id, path_prefix)
);

CREATE TYPE llm_provider_kind AS ENUM ('anthropic', 'openai', 'azure_openai', 'bedrock', 'vertex', 'openai_compat', 'ollama', 'external_cli');
CREATE TYPE llm_feature AS ENUM ('docgen', 'qa', 'decode', 'triage', 'embedding', 'suggest');

CREATE TABLE llm_providers (
    id              uuid PRIMARY KEY,
    kind            llm_provider_kind NOT NULL,
    name            text NOT NULL UNIQUE,
    base_url        text NOT NULL DEFAULT '',
    key_ciphertext  bytea,
    extra           jsonb NOT NULL DEFAULT '{}',   -- region, deployment, project, command template
    redact_pii      boolean NOT NULL DEFAULT false,
    enabled         boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE model_routes (
    feature               llm_feature PRIMARY KEY,
    provider_id           uuid NOT NULL REFERENCES llm_providers(id),
    model                 text NOT NULL,
    max_output_tokens     integer NOT NULL DEFAULT 2048,
    context_token_budget  integer NOT NULL DEFAULT 16000,
    temperature           real NOT NULL DEFAULT 0.2,
    fallback_provider_id  uuid REFERENCES llm_providers(id),
    fallback_model        text NOT NULL DEFAULT '',
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE cost_table (
    provider_kind        llm_provider_kind NOT NULL,
    model                text NOT NULL,
    input_per_mtok_usd   numeric(12, 6) NOT NULL DEFAULT 0,
    output_per_mtok_usd  numeric(12, 6) NOT NULL DEFAULT 0,
    embed_per_mtok_usd   numeric(12, 6) NOT NULL DEFAULT 0,
    updated_by           uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_kind, model)
);

CREATE TABLE embedding_lock (
    index_name     text PRIMARY KEY,
    provider_kind  llm_provider_kind NOT NULL,
    model          text NOT NULL,
    dimensions     integer NOT NULL CHECK (dimensions > 0),
    version        integer NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now()
);

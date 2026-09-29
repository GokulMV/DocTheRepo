-- LLM routing refinements (plan § 8.14): per-route reasoning effort, realistic output ceilings (current
-- models spend output tokens on thinking, so small ceilings truncate), and a provenance flag on prices.
ALTER TABLE model_routes ADD COLUMN effort text NOT NULL DEFAULT '';
ALTER TABLE model_routes ALTER COLUMN max_output_tokens SET DEFAULT 8000;
ALTER TABLE model_routes ALTER COLUMN temperature DROP NOT NULL;
ALTER TABLE model_routes ALTER COLUMN temperature DROP DEFAULT;

-- Provider-scoped spend limits count by provider account, not kind (two OpenAI accounts are two budgets).
ALTER TABLE usage_events ADD COLUMN provider_id uuid;
CREATE INDEX usage_events_provider_idx ON usage_events (provider_id, at DESC);
CREATE INDEX usage_events_repo_idx ON usage_events (repo_id, at DESC);

ALTER TABLE cost_table ADD COLUMN source text NOT NULL DEFAULT 'operator';   -- operator | seeded
ALTER TABLE cost_table ADD COLUMN verified_at timestamptz;

-- Anthropic first-party list prices per million tokens as published when this release was built. They are
-- marked "seeded" and unverified: the UI asks operators to confirm them, and operators own keeping them
-- current (the Hub never reads billing APIs). Other providers' prices are entered by the operator.
INSERT INTO cost_table (provider_kind, model, input_per_mtok_usd, output_per_mtok_usd, source) VALUES
    ('anthropic', 'claude-fable-5-1',  10.00, 50.00, 'seeded'),
    ('anthropic', 'claude-opus-5-5',    4.00, 20.00, 'seeded'),
    ('anthropic', 'claude-opus-5',      5.00, 25.00, 'seeded'),
    ('anthropic', 'claude-sonnet-5-5',  2.00, 10.00, 'seeded'),
    ('anthropic', 'claude-haiku-4-5',   1.00,  5.00, 'seeded')
ON CONFLICT DO NOTHING;

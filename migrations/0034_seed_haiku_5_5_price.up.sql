-- Claude Haiku 5.5 list price per million tokens (prompts up to 100K tokens; longer prompts cost 5x and are
-- under-counted here). Seeded and unverified like the others: operators confirm and keep prices current.
INSERT INTO cost_table (provider_kind, model, input_per_mtok_usd, output_per_mtok_usd, source) VALUES
    ('anthropic', 'claude-haiku-5-5', 0.10, 0.50, 'seeded')
ON CONFLICT DO NOTHING;

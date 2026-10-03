-- Prompt-cache tokens are priced apart from plain input: providers bill cache reads well below the input
-- price and cache writes above it. usage_events.input_tokens stays the total input (token ceilings count
-- every token); the cache columns say how much of it was read from or written to the cache.
ALTER TABLE usage_events ADD COLUMN cache_read_tokens bigint NOT NULL DEFAULT 0;
ALTER TABLE usage_events ADD COLUMN cache_write_tokens bigint NOT NULL DEFAULT 0;

-- NULL: priced as plain input (the old behaviour) until the operator sets them.
ALTER TABLE cost_table ADD COLUMN cache_read_per_mtok_usd numeric(12, 6);
ALTER TABLE cost_table ADD COLUMN cache_write_per_mtok_usd numeric(12, 6);

-- Anthropic bills cache reads at 0.1x and 5-minute cache writes at 1.25x the input price.
UPDATE cost_table
SET cache_read_per_mtok_usd = input_per_mtok_usd * 0.1, cache_write_per_mtok_usd = input_per_mtok_usd * 1.25
WHERE provider_kind = 'anthropic' AND source = 'seeded';

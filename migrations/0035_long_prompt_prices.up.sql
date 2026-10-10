-- Some models bill a long prompt at a higher rate for the whole request. A prompt (input + cache read +
-- cache write tokens) over long_prompt_threshold_tokens is priced at the long_* rates: input, output and
-- cache. NULL threshold: one rate for every prompt (the old behaviour). A NULL long cache price falls back
-- to the long input price, like the base cache prices do.
ALTER TABLE cost_table ADD COLUMN long_prompt_threshold_tokens integer;
ALTER TABLE cost_table ADD COLUMN long_input_per_mtok_usd numeric(12, 6);
ALTER TABLE cost_table ADD COLUMN long_output_per_mtok_usd numeric(12, 6);
ALTER TABLE cost_table ADD COLUMN long_cache_read_per_mtok_usd numeric(12, 6);
ALTER TABLE cost_table ADD COLUMN long_cache_write_per_mtok_usd numeric(12, 6);

-- Claude Haiku 5.5 prompts over 100K tokens cost 5x, cache included.
UPDATE cost_table
SET long_prompt_threshold_tokens = 100000,
    long_input_per_mtok_usd = input_per_mtok_usd * 5,
    long_output_per_mtok_usd = output_per_mtok_usd * 5,
    long_cache_read_per_mtok_usd = cache_read_per_mtok_usd * 5,
    long_cache_write_per_mtok_usd = cache_write_per_mtok_usd * 5
WHERE provider_kind = 'anthropic' AND model = 'claude-haiku-5-5' AND source = 'seeded';

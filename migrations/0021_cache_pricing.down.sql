ALTER TABLE cost_table DROP COLUMN cache_write_per_mtok_usd;
ALTER TABLE cost_table DROP COLUMN cache_read_per_mtok_usd;
ALTER TABLE usage_events DROP COLUMN cache_write_tokens;
ALTER TABLE usage_events DROP COLUMN cache_read_tokens;

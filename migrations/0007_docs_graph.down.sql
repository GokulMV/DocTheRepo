DROP TABLE IF EXISTS prs;
DROP TABLE IF EXISTS manifest_renames;
DROP TABLE IF EXISTS shelf_items;
DROP TYPE IF EXISTS shelf_item_type;
DROP TABLE IF EXISTS library_shelves;
DROP TABLE IF EXISTS edges;
DROP TABLE IF EXISTS entities;
DROP TABLE IF EXISTS doc_nodes;
DROP TYPE IF EXISTS doc_node_kind;
ALTER TABLE embedding_lock
    DROP COLUMN IF EXISTS pending_version,
    DROP COLUMN IF EXISTS pending_provider_kind,
    DROP COLUMN IF EXISTS pending_model,
    DROP COLUMN IF EXISTS pending_dimensions;
ALTER TABLE repos
    DROP CONSTRAINT IF EXISTS repos_on_reject_check,
    DROP COLUMN IF EXISTS pr_stale_after,
    DROP COLUMN IF EXISTS pr_conflict_strategy;

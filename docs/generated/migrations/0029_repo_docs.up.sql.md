<!-- dth:generated source="migrations/0029_repo_docs.up.sql" — edit only inside dth:human blocks -->
# `migrations/0029_repo_docs.up.sql`

Migration to create repo_docs and file_cards tables for storing generated repository documentation with source tracking and confidence metrics.

<!-- dth:chunk 4b19d5a0a9595f19 -->
## `migrations/0029_repo_docs.up.sql`

Creates two tables for documentation storage in v2. The `repo_docs` table stores generated documents per repository with metadata including type, title, sections (as JSONB), confidence scores, and generation tracking (model, tokens, cost). The `file_cards` table stores shape and card data for source files, enabling document generation to track which files contributed to each document via file_hashes. Together they enable per-repository documentation with confidence-weighted outputs and reproducible generation through source tracking (source_sha, inputs_hash).

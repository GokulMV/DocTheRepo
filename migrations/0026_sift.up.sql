-- Ask evidence sifter: a route for cheap yes/no relevance judgments, and the savings they produce.
ALTER TYPE llm_feature ADD VALUE IF NOT EXISTS 'sift';
ALTER TYPE savings_kind ADD VALUE IF NOT EXISTS 'evidence_sifted';

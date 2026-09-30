-- Phase 11.5: the decision route (typed questions → per-option probabilities).
ALTER TYPE llm_feature ADD VALUE IF NOT EXISTS 'decide';
-- A confident decision that skipped a full decode.
ALTER TYPE savings_kind ADD VALUE IF NOT EXISTS 'decision_gate';

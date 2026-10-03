-- opencode (a doc-generation engine run through the DocGen contract) and GitHub Models (OpenAI-compatible
-- inference with a GitHub token) get their own provider kinds, so they are easy to find and label.
ALTER TYPE llm_provider_kind ADD VALUE IF NOT EXISTS 'opencode';
ALTER TYPE llm_provider_kind ADD VALUE IF NOT EXISTS 'github_models';

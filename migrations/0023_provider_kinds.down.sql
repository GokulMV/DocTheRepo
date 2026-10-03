-- PostgreSQL cannot drop an enum value; remove these providers instead.
DELETE FROM llm_providers WHERE kind::text IN ('opencode', 'github_models');

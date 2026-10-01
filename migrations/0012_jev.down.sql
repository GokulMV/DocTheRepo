-- PostgreSQL cannot drop an enum value; remove Jev providers instead (routes on them cascade or fail closed).
DELETE FROM llm_providers WHERE kind::text = 'jev';

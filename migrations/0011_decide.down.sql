-- PostgreSQL cannot drop an enum value; routes and usage rows for 'decide' are removed instead.
DELETE FROM model_routes WHERE feature::text = 'decide';

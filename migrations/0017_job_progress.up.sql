-- Live progress of a running job (for example, files documented so far), shown in Activity and Docs.
ALTER TABLE jobs ADD COLUMN progress jsonb;

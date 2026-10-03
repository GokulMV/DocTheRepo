<!-- dth:generated source="migrations/0019_users_signin.down.sql" — edit only inside dth:human blocks -->
# `migrations/0019_users_signin.down.sql`

<!-- dth:chunk 168c7db8d4831ae5 -->
## `migrations/0019_users_signin.down.sql`

This down-migration rollback script drops the `auth_settings` and `user_invites` tables that were created in the corresponding up-migration. Used to undo schema changes when reverting the sign-in feature migration.

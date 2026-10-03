<!-- dth:generated source="internal/store/gen/users.sql.go" — edit only inside dth:human blocks -->
# `internal/store/gen/users.sql.go`

<!-- dth:chunk c31d148432bc3c55 -->
## `CreateInviteParams`

Parameters struct for creating a user invite. Contains the token hash, target user ID, optional creator ID, and expiration time.

<!-- dth:chunk fad22433245534a0 -->
## `Queries.CreateInvite`

Inserts a new user invite record with the provided parameters. Returns an error if the database operation fails.

<!-- dth:chunk 60822a0f4b1e1c9b -->
## `Queries.DeleteUser`

Deletes a user by ID from the database. Returns the number of rows affected and any error encountered.

<!-- dth:chunk 4f88822d02419af8 -->
## `Queries.GetAuthSettings`

Retrieves the singleton authentication settings record (ID=1) containing password and OIDC configuration. Returns an AuthSetting struct or an error if not found.

<!-- dth:chunk 153fda11affe47cd -->
## `GetInviteRow`

Represents the result of querying an invite with joined user data. Includes the target user's ID, email, name, disabled status, and the invite's expiration and usage timestamps.

<!-- dth:chunk a8e1f5cb5be29193 -->
## `Queries.GetInvite`

Retrieves invite details by token hash, joining with the target user record to return combined invite and user information. Returns GetInviteRow or an error if not found.

<!-- dth:chunk 014f8ed06c18014c -->
## `Queries.PendingInviteUsers`

Queries user IDs with active (unused and non-expired) invites. Returns a slice of user IDs or an error; propagates row scanning errors.

<!-- dth:chunk 83bbd0e6942e6f32 -->
## `UpdateUserParams`

Parameters struct for updating an existing user. Supports optional updates to role, disabled status, and name fields, with the user ID required to identify the target record.

<!-- dth:chunk a0f9a6ea3f102990 -->
## `Queries.UpdateUser`

Updates a user's role, disabled status, and name (using SQL COALESCE to preserve existing values for NULL fields). Returns the updated User record or an error.

<!-- dth:chunk 4259f6f473c7e9ca -->
## `UpsertAuthSettingsParams`

Parameters struct for upserting authentication settings. Contains optional password enablement flag and OIDC provider configuration fields including issuer, client ID, encrypted secret, allowed domains, and groups claim name.

<!-- dth:chunk 3511356a8a6907dd -->
## `Queries.UpsertAuthSettings`

Inserts or updates the singleton authentication settings record (ID=1) with the provided parameters, including password and OIDC configuration. Updates timestamp is automatically set to current time.

<!-- dth:chunk 5d6629323c008092 -->
## `Queries.UseInvites`

Accepting one link retires every open link for that person.

<!-- dth:chunk de0d6a5d858077c1 -->
## `__module__`

SQL query constant definitions for user management operations including invites, authentication settings, sessions, tokens, and user records.

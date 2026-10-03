<!-- dth:generated source="web/src/pages/Users.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Users.tsx`

Manages user administration including listing, adding, editing, and configuring access for users and their sign-in methods.

<!-- dth:chunk 98310e47a4d76913 -->
## `InviteLink`

Represents a one-time password reset or invite link that a user can use to set their password.

<!-- dth:chunk cfc7cc7cb8e592df -->
## `LinkResult`

Displays the result of creating an invite link, showing whether it was successfully emailed or if it failed. If emailing failed, displays the error and shows the raw link for manual sharing. Includes the link expiration date and notes about link security.

<!-- dth:chunk 9b8cb3bfef7d99cb -->
## `AddUser`

Dialog for adding a new user to the system. Collects email, optional name, and role selection. Optionally sends an invite link via email (if mail is enabled) or shows a link to share manually. On success, displays a confirmation with the generated invite link if applicable, and notes about available sign-in methods.

<!-- dth:chunk 7e4e858c488bbb5f -->
## `EditName`

Dialog for editing a user's display name. The email cannot be changed here; users must delete and re-add the user to change their email. Notes that SSO users will have their name refreshed from the identity provider on each sign-in.

<!-- dth:chunk ef4490affcc1fc68 -->
## `ResetLink`

Dialog for creating a one-time password link for a user. For users with existing passwords, this resets their password and signs them out everywhere. For new users, creates their initial password link. Automatically emails the link if mail is enabled.

<!-- dth:chunk 67aa42f6cc6c9628 -->
## `RepoAccess`

Dialog for managing which repositories a specific user can access. Displays checkboxes for all repositories; admins and above can see all repositories regardless. Loads current access grants on open and saves changes via API. Replaces previous direct grants but preserves identity-provider-group-based access.

<!-- dth:chunk d19086dacd61bbe2 -->
## `SignInSummary`

Summary card showing which sign-in methods are enabled (SSO and/or password), with brief explanations of how each works. Provides a test-email button and link to sign-in settings for owners. Shows status of email configuration.

<!-- dth:chunk 31f8e49195dee477 -->
## `signInStatus`

Returns a human-readable status string and visual tone ('gray'|'amber'|'green'|'blue') for a user's sign-in state. Prioritizes SSO+password > SSO > password > pending invite > never signed in.

<!-- dth:chunk 7171e4376df00dc5 -->
## `Users`

Main users management page. Lists all users with their role, sign-in status, and last login time. Provides buttons to manage repository access, edit name, reset password, disable/enable, and remove users. Only owners can modify owners. Shows sign-in method summary at top and supports adding new users via dialog.

<!-- dth:chunk d2365d7a7756680d -->
## `__module__`

Array defining the four user roles and their capabilities: viewer (read-only access), editor (editing and syncs), admin (infrastructure management), and owner (full system control).

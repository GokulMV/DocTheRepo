<!-- dth:generated source="web/src/pages/Invite.tsx" — edit only inside dth:human blocks -->
# `web/src/pages/Invite.tsx`

Provides the public invite/password-reset page where new users or those resetting passwords set their credentials and are signed in.

<!-- dth:chunk 1b0fc7f8926e2273 -->
## `InviteInfo`

Data structure describing an invite or password-reset link that a user can sign up or change their credentials with. Contains the invited user's email and name, when the link expires, whether password-based sign-in is enabled, and whether SSO is available.

<!-- dth:chunk ea7d76b8e4d20dc2 -->
## `Invite`

Public sign-up and password-reset page accessed via invite token from the URL. Fetches invite details from the server, displays a welcome message with the invitee's name and email, and presents a password form if password-based sign-in is enabled. On valid submission, posts the password to the server, refreshes authentication state, and navigates to `/ask`. Handles expired or already-used links by showing an error message with a link back to sign-in. Requires passwords to be at least 12 characters and match in both fields.

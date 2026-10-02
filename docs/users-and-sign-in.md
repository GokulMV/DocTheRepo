# Users and sign-in

Admins manage people under **Users & access**. Owners choose how people sign in under **Sign-in & SSO**.

## Roles

| Role | Can |
|---|---|
| viewer | Read docs and ask questions in the repositories they can see |
| editor | Also edit docs, triage issues, run syncs |
| admin | Also manage connectors, models, spend and users; sees every repository |
| owner | Everything, including other owners and the sign-in settings |

Viewers and editors see the repositories granted to them, directly (**Repositories** on the user's row) or
through identity-provider groups. If `auth.all_users_read_all_repos` is set, everyone sees every repository.

## How people sign in

There are two methods. Turn on either one or both.

- **Email and password.** On by default in local mode (`./scripts/quickstart.sh`, `dth up`).
- **Single sign-on (OIDC).** Google Workspace, Microsoft Entra ID, Okta, Keycloak, or any OpenID Connect
  provider.

The Hub refuses a change that would leave no way to sign in. You can't turn passwords off until single
sign-on works, and you can't remove single sign-on while passwords are off.

### Adding someone

1. **Users & access → Add user.** Enter their email, name and role.
2. With passwords on, keep "Create a link for them to set a password" ticked. The Hub shows a one-time
   link. Send it to them however you normally would (the Hub doesn't send email). They open it, choose a
   password of at least 12 characters, and are signed in. The link works once and expires after 7 days.
3. With single sign-on on, they can also sign in with it using the same email address. The role you chose
   applies from their first sign-in.

**Forgotten password.** Use the link icon on their row to create a new link. When they use it, the old
password stops working and they are signed out everywhere. An admin can't do this for an owner; only an
owner can.

**Disable** stops someone signing in and ends their sessions and tokens, but keeps their data.
**Remove** deletes the user with their questions, tokens and grants; the audit log keeps the actions it
recorded. Nobody can remove themselves, and the last owner can't be removed, demoted or disabled.

With single sign-on, anyone your identity provider admits from an allowed email domain can sign in. They
start as a viewer. To give someone another role from the start, add them first.

### Setting up single sign-on

**Sign-in & SSO** walks through it for each provider:

1. Pick your identity provider.
2. Create a web (confidential) OpenID Connect app there. Register the **callback URL** shown on the page,
   which is `<public URL>/api/v1/auth/callback`. Most providers require https, so serve the Hub over https
   and set `DTH_PUBLIC_URL`.
3. Copy the issuer URL, client ID and client secret into the page, plus the allowed email domains.
   **Check and save** loads the provider's `/.well-known/openid-configuration` first, so a wrong issuer
   never replaces a working setup.

| Provider | Issuer URL | Notes |
|---|---|---|
| Google Workspace | `https://accounts.google.com` | Use an "Internal" consent screen and set your domain under allowed domains; otherwise any Google account could sign in. Google sends no groups. |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant-id>/v2.0` | Add the optional `email` claim to the ID token (Token configuration). For groups, add a groups claim and enter `groups` (Entra sends group IDs). |
| Okta | `https://<org>.okta.com` | Assign the people or groups who may use the app. For groups, add a `groups` claim to the ID token. |
| Keycloak | `https://<host>/realms/<realm>` | Turn on client authentication. For groups, add a Group Membership mapper named `groups`. |

The client secret is sealed in the browser to the Hub's key, then stored encrypted. It is never shown
again; leave the field empty to keep it.

### Locked out

If single sign-on breaks while passwords are off:
1. Restart the Hub with `DTH_SETTINGS='auth: {password: true}'`.
2. Run `dth-hub invite <your email>` where the Hub runs, for example
   `docker compose exec hub /dth-hub invite you@acme.com` or
   `kubectl exec deploy/dth-api -- /dth-hub invite you@acme.com`. It uses the Hub's own database and public
   URL, creates the user if needed (as owner), and prints a one-time password link.

### Single sign-on in the config file

Deployments can also configure single sign-on in the config file or environment (`auth.mode: oidc`,
`auth.oidc.*`, the Helm `auth.oidc` values and the Terraform variables). Settings saved under
**Sign-in & SSO** take precedence. Removing them there switches back to the config file's settings.

## Endpoints

| Endpoint | Role |
|---|---|
| `POST /api/v1/users` (`email`, `name`, `role`, `invite`) | admin |
| `PATCH /api/v1/users/{id}` (`name`, `role`, `disabled`) | admin |
| `DELETE /api/v1/users/{id}` | admin |
| `POST /api/v1/users/{id}/invite` (new password link) | admin |
| `GET` / `POST /api/v1/auth/invite/{token}` (check a link, set a password) | public; the token is the credential |
| `GET` / `PUT /api/v1/auth/settings` | owner |

Every change is written to the audit log: `user.create`, `user.update`, `user.invite`, `user.delete`,
`auth.password_set` and `auth.settings`.

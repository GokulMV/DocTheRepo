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

### The first owner

No owner password is ever printed to a console or a log:

| Where the Hub runs | How the first owner gets in |
|---|---|
| Your machine (`quickstart.sh`, `dth up`) | A one-time link to choose your password, opened in the browser. A random bootstrap password signs the tool in once to create an API token and the link, then is deleted; it stops working when you use the link. |
| Cloud, with single sign-on (recommended) | List the owners in the settings file (`users: [{email: you@acme.com, role: owner}]`, see [settings-file.md](settings-file.md)); they sign in with SSO. No password exists. |
| Cloud, passwords only | `dth-hub invite you@acme.com owner` where the Hub runs (`kubectl exec deploy/dth-api -- /dth-hub invite …`) prints a one-time link to the operator's terminal only. The Terraform modules' local mode keeps a generated owner password in Secrets Manager / Secret Manager instead; change it with a link after the first sign-in. |

Avoid putting `DTH_OWNER_PASSWORD` in CI logs, Compose files or Helm values: it is only read while the
database has no users, and a link is safer.

### Adding someone

1. **Users & access → Add user.** Enter their email, name and role.
2. With passwords on, keep the password link ticked. With [email](#email) set up, the Hub emails them a
   one-time link; otherwise it shows the link for you to send however you normally would. They open it,
   choose a password of at least 12 characters, and are signed in. The link works once and expires after
   7 days. The link is shown either way, so you can still pass it on if the email does not arrive.
3. With single sign-on on, they can also sign in with it using the same email address. The role you chose
   applies from their first sign-in.

**Forgotten password.** Use the link icon on their row to create a new link (emailed when email is set
up). When they use it, the old
password stops working and they are signed out everywhere. An admin can't do this for an owner; only an
owner can.

**Disable** stops someone signing in and ends their sessions and tokens, but keeps their data.
**Remove** deletes the user with their questions, tokens and grants; the audit log keeps the actions it
recorded. Nobody can remove themselves, and the last owner can't be removed, demoted or disabled.

With single sign-on, anyone your identity provider admits from an allowed email domain can sign in. They
start as a viewer. To give someone another role from the start, add them first.

### Email

The Hub can email invite and password links through any SMTP service: Google Workspace, Microsoft 365,
Amazon SES, SendGrid, Mailgun, Postmark or your own mail server. Set two variables and restart:

| Variable | Example |
|---|---|
| `DTH_SMTP_URL` | `smtp://user:password@smtp.example.com:587` (STARTTLS, required), `smtps://user:password@smtp.example.com:465` (TLS from the start), or `smtp://relay.internal:25?starttls=off` for a trusted relay inside your network |
| `DTH_EMAIL_FROM` | `DocTheRepo <docs@example.com>`: an address your provider lets you send from |

Percent-encode special characters in the user name and password (`@` → `%40`, `:` → `%3A`). The URL is
read from the environment only, since it holds a password (`email.smtp_url_env` in `dth.yaml` names
another variable; `email.from` sets the sender). Helm: `email.from` plus `email.existingSecret` holding
the URL. Compose: put both variables in `.env`.

| Provider | Host and port | User / password |
|---|---|---|
| Amazon SES | `email-smtp.<region>.amazonaws.com:587` | SMTP credentials made in the SES console (not your AWS keys) |
| SendGrid | `smtp.sendgrid.net:587` | `apikey` / your API key |
| Mailgun | `smtp.mailgun.org:587` | The domain's SMTP login |
| Postmark | `smtp.postmarkapp.com:587` | Server API token as both |
| Google Workspace | `smtp-relay.gmail.com:587` (SMTP relay) or `smtp.gmail.com:587` | Relay: allow the Hub's IP in the admin console; `smtp.gmail.com`: an app password |
| Microsoft 365 | `smtp.office365.com:587` | A mailbox with SMTP AUTH turned on |

An owner can check it with **Users & access → Send test email**, which sends one to their own address.
When a message is refused, the error is shown next to the link, so the admin can pass the link on.

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

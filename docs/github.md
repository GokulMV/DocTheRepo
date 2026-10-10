# Connecting GitHub

## One click (recommended)

**Settings → Connections → Connect with GitHub.**

1. **Create the app.** GitHub opens its "Create GitHub App" page, already filled in for this Hub. Click
   **Create GitHub App**. To create it in an organization you administer, or on GitHub Enterprise Server,
   first open **Organization or GitHub Enterprise** and fill those in.
2. **Install it.** GitHub hands the Hub the app's ID, private key, webhook secret, client ID and client
   secret (stored encrypted; nobody has to copy them). It then shows the install page: choose **All
   repositories** or the ones you want.
3. **Pick repositories.** Back in the Hub, tick the repositories to document and click **Track**.

What the app may do:

| Permission | Why |
|---|---|
| Contents: read & write | Read code; write generated docs. The Hub only writes under each repository's docs path. |
| Pull requests: read & write | Open and update docs PRs (in the PR push modes) |
| Metadata, commit statuses, checks: read | Branches, and waiting for checks before merging a docs PR |
| Members: read | Repository access from GitHub teams |
| Issues, Actions: read | Live lookups in GitHub (the GitHub MCP server) when an admin signs in with the app |

**Signing in with the app.** The app's **Callback URL** is the Hub's MCP sign-in address
(`<DTH_PUBLIC_URL>/api/v1/mcp/oauth/callback`), so admins can connect **Live lookups in GitHub** by
signing in on GitHub instead of pasting a token. See [MCP connections](mcp-connections.md#github). GitHub
does not ask for this sign-in during the install.

**Webhooks:**
- **Public `DTH_PUBLIC_URL`:** the app sends push and pull-request events to
  `/hooks/github/<connector id>`, signed with the generated secret.
- **Private address (`localhost`, a private network):** GitHub cannot reach the Hub, so the app is created
  without a webhook and the connector polls instead.

**Requirements:** `DTH_PUBLIC_URL` must be the address you use in the browser, because GitHub sends you back
there. The flow's state is encrypted with the Hub's key, is bound to your account, expires after 30 minutes,
and works only once.

**Changing repositories later:** use the app's **Configure** page on GitHub. The connector picks up the change
on its own.

## Use an App you already have

GitHub App names are unique across GitHub, and uninstalling an App doesn't delete it. To reuse an App
(for example one an earlier **Connect with GitHub** created), choose **Settings → Connections → GitLab, a token, or GitHub Enterprise with your own app →
Already created a DocTheRepo app on GitHub? Use it instead**:

1. Open **GitHub → Settings → Developer settings → GitHub Apps** (for an organization: the org's
   **Settings → GitHub Apps**) and click **Edit** next to the App.
2. Copy the **App ID**.
3. Under **Private keys**, click **Generate a private key** and choose the downloaded `.pem` file in
   the Hub. GitHub never shows an older key again, so a new one is needed.

4. Optional, for **Live lookups in GitHub** with GitHub sign-in: open **Also sign in to GitHub's MCP
   server with this app**. On the App's page, copy the **Client ID**, click **Generate a new client
   secret**, and add the callback URL the Hub shows (`<public URL>/api/v1/mcp/oauth/callback`) under
   **Callback URL**. Enter the client ID and secret in the Hub. The secret is sealed in your browser.

The Hub signs in as the App and finds where it is installed. If it isn't installed, the Hub links to its
install page, and **I've installed it** finishes the connection. If it's installed on several accounts,
you choose one. These connectors poll, because GitHub keeps sending the App's webhooks to the URL set on
the App.

An App connected before the Hub kept client credentials (an earlier **Connect with GitHub**, or this form
without step 4) can get them later from the GitHub entry under **Look things up live (MCP)**, or with
`PUT /api/v1/github/connect/{connector id}/oauth-client` (`client_id`, sealed `client_secret`; both empty
removes them). Its permissions do not change on their own. For the read-only lookups, add **Issues: read**
and **Actions: read** on the App's **Permissions & events** page. Each installation then has to accept
the new permissions on GitHub.

To start fresh instead, delete the old App on its settings page (**Advanced → Delete GitHub App**). New
Apps get a unique name such as `DocTheRepo-acme-3f9a1c`, which you can change on GitHub before creating
the App.

## Disable and remove

For a connector created as a GitHub App (one click, or by hand with an App key):

| In the Hub | On GitHub |
|---|---|
| **Disable** | The App installation is **suspended**. GitHub sends no events and the App cannot read the repositories. |
| **Enable** | The installation is resumed. |
| **Remove** | The App is **uninstalled** from the account or organization, so its access ends at once. Then the connector and its repositories are removed from the Hub. |

GitHub has no API for one app to delete another, so after **Remove** the App itself still exists, with no
installations. The Hub links to its settings page: **Advanced → Delete GitHub App**.

If GitHub refuses (for example, the App was already deleted there), the Hub still makes the change and
shows the reason. Token connectors have nothing to suspend on GitHub. Revoke the token in GitHub's
settings.

## By hand

The same dialog takes the details directly:

- **A fine-grained personal access token:** contents and pull requests read & write, metadata read.
- **An existing GitHub App:** app ID, installation ID and private key.
- **GitLab:** a token with the `api` scope.

The settings file accepts the same fields (`docs/settings-file.md`).

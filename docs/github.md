# Connecting GitHub

## One click (recommended)

**Connectors → Connect git host → Connect with GitHub.**

1. **Create the app.** GitHub opens its "Create GitHub App" page, already filled in for this Hub. Click
   **Create GitHub App**. To create it in an organization you administer, or on GitHub Enterprise Server,
   first open **Organization or GitHub Enterprise** and fill those in.
2. **Install it.** GitHub hands the Hub the app's ID, private key and webhook secret (stored encrypted;
   nobody has to copy them). It then shows the install page: choose **All repositories** or the ones you
   want.
3. **Pick repositories.** Back in the Hub, tick the repositories to document and click **Track**.

What the app may do:

| Permission | Why |
|---|---|
| Contents: read & write | Read code; write generated docs. The Hub only writes under each repository's docs path. |
| Pull requests: read & write | Open and update docs PRs (in the PR push modes) |
| Metadata, commit statuses, checks: read | Branches, and waiting for checks before merging a docs PR |
| Members: read | Repository access from GitHub teams |

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

## By hand

The same dialog takes the details directly:

- **A fine-grained personal access token:** contents and pull requests read & write, metadata read.
- **An existing GitHub App:** app ID, installation ID and private key.
- **GitLab:** a token with the `api` scope.

The settings file accepts the same fields (`docs/settings-file.md`).

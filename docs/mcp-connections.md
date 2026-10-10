# MCP connections

Ask can look things up live in other products while it answers, through each product's MCP server. Examples: the errors Sentry sees right now, Datadog logs, Jira tickets, AWS or Google Cloud resources.

Set them up under **Settings → Connections → Look things up live (MCP)**.

## How it works

1. **Ask decides to use the tools.** A question goes to the connected tools when:
   - it is about live state (errors, alerts, logs, cost, tickets, deploys, "today", "right now"), or
   - it names a connected product.

   Other questions use the tools only when the index finds too little.
2. **The agent calls them.** It picks a tool, fills in its arguments, reads the result, and can call another tool or search the code. That takes at most 6 steps.
3. **Results are cited.** Each result is a source in the answer, shown as **live** with the connection's name.
4. **Nothing is stored or cached.** Live answers describe a moment, so tool results are never kept and those answers are never cached.

**Safety:**

- **Only allowed tools are called.** By default those are the tools the server marks read-only. Under **Tools**, an admin can turn any tool on or off. Tools that may change things start off, and turning one on shows a warning.
- **Who can use it.** Each connection sets the lowest role that may use it in Ask. The default is editors and above. Answers can include what the connection returns.
- **Tool results are treated as data,** never as instructions.
- **Secrets are sealed.** Tokens, keys and sign-in tokens are sealed in the browser and stored encrypted, as with every other secret. They are also covered by `dth-hub rotate-key`.

## Ways to sign in

| Method | When | What you do |
|---|---|---|
| **Sign in with the product (OAuth)** | Most hosted servers | Click **Connect and sign in**, approve in the product, and you come back. The Hub registers itself with the product and refreshes the sign-in on its own. If the sign-in lapses, the connection shows **Needs sign-in**. |
| **Sign in with GitHub (the Hub's GitHub App)** | GitHub, when the Hub has a GitHub App with its client ID and secret | Click **Sign in with GitHub**, approve on GitHub's page, and you come back connected. See [GitHub](#github). |
| **A token** | GitHub (without a GitHub App), Splunk, Dynatrace, Elastic, Stripe | Paste a read-only token. Values that already name a scheme (`ApiKey …`) are sent as they are. |
| **A key in a header** | New Relic (`api-key`) | Paste the key. The header name is filled in. |
| **AWS credentials** | AWS-hosted MCP servers | Requests are signed with SigV4. The always-on choice is **A read-only role in your AWS account**: click **Create a read-only role in AWS**, create the stack in the AWS console, enter the account ID and click **Check access**. The Hub assumes that role with its External ID and signs with the role's credentials ([details](connectors.md#read-only-role-in-aws)). This needs a Hub running on AWS. You can also pick **The IAM role the Hub runs with** (attach read-only policies to it) or **Access keys**. The region and signing service are read from the address (`aws-mcp.us-east-1.api.aws` → `us-east-1`, `aws-mcp`) and can be changed under More options. |
| **Google Cloud service account** | Google Cloud MCP servers | On GKE, Cloud Run or Compute Engine, leave **Use the service account the Hub runs with** on and give it viewer roles. Otherwise paste a service account key. If your organization requires user sign-in instead, choose OAuth and enter your own OAuth client ID. |
| **No sign-in** | Public servers such as AWS documentation | Nothing to do. |

OAuth needs an address the product can send your browser back to. The Hub uses `server.public_url` if it is set, otherwise the address you opened the Hub at. It must be https, or localhost. If a product does not let apps register themselves (Google Cloud, Slack), create an OAuth app there. Use the redirect address shown under **More options**, then enter the app's client ID and secret.

## Products

Addresses come from each vendor's documentation, checked in October 2026. Entries marked "check" could not be fully confirmed: compare them with the vendor's page. Every address can be edited.

| Product | Address | Sign-in | Notes |
|---|---|---|---|
| AWS | `https://aws-mcp.us-east-1.api.aws/mcp` | AWS (SigV4) | Its scope is what the IAM role allows |
| AWS documentation | `https://knowledge-mcp.global.api.aws` | None | Public |
| Google Cloud | `https://<product>.googleapis.com/mcp` (logging, monitoring, run, container, bigquery, compute, cloudresourcemanager) | Google / OAuth | One connection per product |
| Azure DevOps | `https://mcp.dev.azure.com/<organization>` | OAuth (Entra ID) | |
| Sentry | `https://mcp.sentry.dev/mcp` | OAuth | |
| Datadog | `https://mcp.<site>/api/unstable/mcp-server/mcp` | OAuth or token | Pick your site (US1, US3, US5, EU1, AP1, AP2). Check. |
| Grafana Cloud | `https://mcp.grafana.com/mcp` | OAuth | Also asks for your stack address (`X-Grafana-URL`) |
| PagerDuty | `https://mcp.pagerduty.com/mcp` (EU: `mcp.eu.pagerduty.com`) | OAuth | |
| New Relic | `https://mcp.newrelic.com/mcp/` (EU: `mcp.eu.newrelic.com`) | `api-key` header | |
| Honeycomb | `https://mcp.honeycomb.io/mcp` (EU: `mcp.eu1.honeycomb.io`) | OAuth | |
| Splunk | `https://<host>:8089/services/mcp` | Token | Install the MCP Server app first |
| Elastic | `<kibana>/api/agent_builder/mcp` | `ApiKey <key>` | |
| Dynatrace | `https://<env>.apps.dynatrace.com/platform-reserved/mcp-gateway/v0.1/servers/dynatrace-mcp/mcp` | Token | |
| Cloudflare | `https://observability.mcp.cloudflare.com/mcp` | OAuth | Check |
| Jira and Confluence | `https://mcp.atlassian.com/v2/mcp` | OAuth or API token | Check |
| Notion | `https://mcp.notion.com/mcp` | OAuth | |
| Linear | `https://mcp.linear.app/mcp/readonly` | OAuth or API key | Read-only address by default |
| Slack | `https://mcp.slack.com/mcp` | OAuth with your Slack app | |
| GitHub | `https://api.githubcopilot.com/mcp/` | Sign in with GitHub (the Hub's GitHub App) or token | See [GitHub](#github) |
| GitLab | `https://gitlab.com/api/v4/mcp` | OAuth | Beta, Premium and Ultimate |
| Supabase | `https://mcp.supabase.com/mcp?read_only=true` | OAuth | |
| Stripe | `https://mcp.stripe.com` | Token or OAuth | Preview |

### GitHub

GitHub's MCP server (`https://api.githubcopilot.com/mcp/`) takes GitHub OAuth tokens, including a GitHub
App's user access tokens. It does not let apps register themselves, so the Hub signs in with **its own
GitHub App**: the one **Connect with GitHub** created (see [Connecting GitHub](github.md)).

**With a GitHub App that has a client ID and secret,** the GitHub tile offers **Sign in with GitHub**:

1. GitHub's sign-in page opens (`github.com/login/oauth/authorize`, with the App's client ID, a one-time
   state and PKCE).
2. You approve the App and GitHub sends you back to the Hub's MCP sign-in address, `/api/v1/mcp/oauth/callback`.
   That address must be one of the App's **Callback URLs**. Apps created by **Connect with GitHub** have
   it already. For other Apps the Hub shows the exact address to add.
3. The Hub exchanges the code with the App's client secret and stores the tokens sealed.

**What the connection can see.** The token acts as **the admin who signed in**, limited twice: to the
repositories **the App is installed on**, and to **the App's permissions**. Ask cannot see a repository
the App can't, even when the admin can, and it can't do anything the App's permissions don't allow, even
when the admin could. For the read-only lookups (issues, pull requests, code, Actions runs) the App needs
**Contents, Issues, Pull requests, Actions and Metadata: read**. New Apps ask for these. On an older App,
add Issues and Actions (read) under **Permissions & events**.

**Staying signed in.** With **user-to-server token expiration** on (GitHub's default for new Apps, under
the App's **Optional features**), a token lasts 8 hours and the Hub renews it with the refresh token. Each
refresh token works once and lasts 6 months, and the Hub keeps the new one each time. If GitHub refuses
the refresh token (it expired, or the admin revoked the App under **Settings → Applications**), the
connection shows **Needs sign-in** until someone signs in again. If the App's client secret changes, the
next renewal uses the one stored with the App.

**Without a GitHub App,** the tile shows the token form and a link: **Connect a GitHub App first** to sign
in with GitHub. If the Hub has an App without a client ID and secret, the tile can add them. On the App's
settings page, add the callback URL, copy the **Client ID** and generate a client secret.

**Not in the list, and why:**

- **Local only (stdio).** These servers run only on your own machine: Azure MCP Server, Snyk, and the AWS Labs servers (CloudWatch, CloudTrail, Cost Explorer and others). To use one, run it behind an HTTP bridge and add it as **Any MCP server**.
- **Vercel** accepts only an allowlist of approved clients.
- **Wiz** has no public endpoint confirmed.

## Troubleshooting

| What you see | Why | What to do |
|---|---|---|
| **Needs sign-in** | Nobody has signed in yet, or the product revoked the sign-in | Click **Sign in** |
| "the server refused the key (401)" | The token is wrong, expired or lacks read access | Replace the token. **Check** reconnects. |
| "this server does not let apps register themselves" | The product does not support dynamic client registration | Create an OAuth app in the product and enter its client ID under More options |
| "sign-in providers only return to https addresses" | The Hub was opened over plain http | Open the Hub over https, or set `server.public_url` |
| GitHub "redirect_uri_mismatch" | The Hub's MCP sign-in address is not a Callback URL of the GitHub App | Add the address shown in the GitHub tile under the App's **Callback URL** |
| GitHub "incorrect_client_credentials" | The App's client secret was deleted or replaced on GitHub | Enter the new client ID and secret in the GitHub tile |
| GitHub connected, but repositories are missing | The App isn't installed on them, or lacks a permission | Change the installation's repositories, or add the permission on the App |
| AWS "refused the signature" | Wrong region or service, or no credentials | Set them under More options. Check that the Hub's role exists. |
| "this Hub has no AWS identity of its own" | The Hub does not run on AWS, so no role can trust it | Use access keys, or sign in with AWS in the browser |
| Connected, but Ask never uses it | No tools are on, or the asker's role is below the connection's minimum | Open **Tools**, or change **Who can use it** |

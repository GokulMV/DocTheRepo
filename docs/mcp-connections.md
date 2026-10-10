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
| **A token** | GitHub, Splunk, Dynatrace, Elastic, Stripe | Paste a read-only token. Values that already name a scheme (`ApiKey …`) are sent as they are. |
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
| GitHub | `https://api.githubcopilot.com/mcp/` | Token | |
| GitLab | `https://gitlab.com/api/v4/mcp` | OAuth | Beta, Premium and Ultimate |
| Supabase | `https://mcp.supabase.com/mcp?read_only=true` | OAuth | |
| Stripe | `https://mcp.stripe.com` | Token or OAuth | Preview |

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
| AWS "refused the signature" | Wrong region or service, or no credentials | Set them under More options. Check that the Hub's role exists. |
| "this Hub has no AWS identity of its own" | The Hub does not run on AWS, so no role can trust it | Use access keys, or sign in with AWS in the browser |
| Connected, but Ask never uses it | No tools are on, or the asker's role is below the connection's minimum | Open **Tools**, or change **Who can use it** |

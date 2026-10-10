/**
 * Hosted MCP servers the Hub can connect to. Addresses and sign-in methods come from each vendor's
 * documentation (checked October 2026); `check` marks entries whose exact address could not be confirmed,
 * so the dialog asks the admin to compare it with the vendor's page. Everything stays editable.
 */
export type McpAuth = 'none' | 'bearer' | 'header' | 'oauth' | 'aws' | 'google';

export interface McpVariant {
  label: string;
  url: string;
}

export interface McpEntry {
  key: string;
  name: string;
  group: 'Cloud' | 'Monitoring' | 'Work' | 'Code' | 'Other';
  /** What Ask can look up there, in a few words. */
  what: string;
  url: string;
  /** Regions or sites: a choice of addresses. */
  variants?: McpVariant[];
  /** Parts of the address the admin fills in, e.g. {host} → "Your Splunk host". */
  placeholders?: Record<string, string>;
  auth: McpAuth;
  /** Other sign-in methods the server accepts. */
  alsoAuth?: McpAuth[];
  headerName?: string;
  /** What the product calls the key. */
  keyName?: string;
  /** Extra non-secret header asked for, e.g. Grafana's stack address. */
  extraHeader?: { name: string; label: string; placeholder: string };
  docs: string;
  note?: string;
  check?: boolean;
}

const GCP = (svc: string) => `https://${svc}.googleapis.com/mcp`;

export const MCP_CATALOG: McpEntry[] = [
  {
    key: 'aws', name: 'AWS', group: 'Cloud', what: 'Your AWS resources, CloudWatch, CloudTrail, costs and docs',
    url: 'https://aws-mcp.us-east-1.api.aws/mcp', auth: 'oauth', alsoAuth: ['aws'],
    docs: 'https://docs.aws.amazon.com/agent-toolkit/latest/userguide/oauth-authentication.html',
    note: 'Sign in on AWS’s own page and approve: Ask can then see what your AWS identity allows (it needs the AWSMCPSignInOAuthAccessPolicy managed policy). AWS keeps a sign-in for up to 12 hours, then asks again. For an always-on connection, sign requests with the Hub’s IAM role instead.',
  },
  {
    key: 'aws-knowledge', name: 'AWS documentation', group: 'Cloud', what: 'AWS docs, regional availability',
    url: 'https://knowledge-mcp.global.api.aws', auth: 'none',
    docs: 'https://github.com/awslabs/mcp/tree/main/src/aws-knowledge-mcp-server', note: 'Public and free. No sign-in.',
  },
  {
    key: 'gcp', name: 'Google Cloud', group: 'Cloud', what: 'Logs, metrics, Cloud Run, GKE, BigQuery and more',
    url: GCP('logging'), auth: 'google', alsoAuth: ['oauth'],
    variants: [
      { label: 'Cloud Logging', url: GCP('logging') }, { label: 'Cloud Monitoring', url: GCP('monitoring') },
      { label: 'Cloud Run', url: GCP('run') }, { label: 'GKE', url: GCP('container') }, { label: 'BigQuery', url: GCP('bigquery') },
      { label: 'Compute Engine', url: GCP('compute') }, { label: 'Resource Manager', url: GCP('cloudresourcemanager') },
    ],
    docs: 'https://docs.cloud.google.com/mcp/supported-products',
    note: 'Uses the Hub’s Google service account (on GKE, Cloud Run or Compute Engine) or a key you paste. Give it viewer roles only. Add one connection per product.',
  },
  {
    key: 'azure-devops', name: 'Azure DevOps', group: 'Code', what: 'Work items, pull requests, pipelines, wikis',
    url: 'https://mcp.dev.azure.com/{organization}', placeholders: { organization: 'Your Azure DevOps organization' }, auth: 'oauth',
    docs: 'https://devblogs.microsoft.com/devops/azure-devops-remote-mcp-server-ga/', note: 'The organization must use Microsoft Entra ID.',
  },
  {
    key: 'sentry', name: 'Sentry', group: 'Monitoring', what: 'Issues, errors, releases, Seer analysis',
    url: 'https://mcp.sentry.dev/mcp', auth: 'oauth', docs: 'https://docs.sentry.io/product/sentry-mcp/',
  },
  {
    key: 'datadog', name: 'Datadog', group: 'Monitoring', what: 'Logs, metrics, traces, monitors, incidents',
    url: 'https://mcp.datadoghq.com/api/unstable/mcp-server/mcp', auth: 'oauth', alsoAuth: ['bearer'], keyName: 'personal or service access token',
    variants: [
      { label: 'US1', url: 'https://mcp.datadoghq.com/api/unstable/mcp-server/mcp' },
      { label: 'US3', url: 'https://mcp.us3.datadoghq.com/api/unstable/mcp-server/mcp' },
      { label: 'US5', url: 'https://mcp.us5.datadoghq.com/api/unstable/mcp-server/mcp' },
      { label: 'EU1', url: 'https://mcp.datadoghq.eu/api/unstable/mcp-server/mcp' },
      { label: 'AP1', url: 'https://mcp.ap1.datadoghq.com/api/unstable/mcp-server/mcp' },
      { label: 'AP2', url: 'https://mcp.ap2.datadoghq.com/api/unstable/mcp-server/mcp' },
    ],
    docs: 'https://docs.datadoghq.com/bits_ai/mcp_server/setup/', note: 'Pick your Datadog site. Datadog marks this server as under active development.', check: true,
  },
  {
    key: 'grafana', name: 'Grafana Cloud', group: 'Monitoring', what: 'Dashboards, Prometheus, Loki, alerts, incidents',
    url: 'https://mcp.grafana.com/mcp', auth: 'oauth',
    extraHeader: { name: 'X-Grafana-URL', label: 'Your Grafana stack address', placeholder: 'https://acme.grafana.net' },
    docs: 'https://grafana.com/docs/grafana-cloud/ai-tools/mcp-servers/cloud-mcp/',
  },
  {
    key: 'pagerduty', name: 'PagerDuty', group: 'Monitoring', what: 'Incidents, services, on-call schedules',
    url: 'https://mcp.pagerduty.com/mcp', auth: 'oauth',
    variants: [{ label: 'US', url: 'https://mcp.pagerduty.com/mcp' }, { label: 'EU', url: 'https://mcp.eu.pagerduty.com/mcp' }],
    docs: 'https://support.pagerduty.com/main/docs/pagerduty-mcp-server',
  },
  {
    key: 'newrelic', name: 'New Relic', group: 'Monitoring', what: 'Entities, NRQL queries, alerts',
    url: 'https://mcp.newrelic.com/mcp/', auth: 'oauth', alsoAuth: ['header'], headerName: 'api-key', keyName: 'User API key (NRAK-…)',
    variants: [{ label: 'US', url: 'https://mcp.newrelic.com/mcp/' }, { label: 'EU', url: 'https://mcp.eu.newrelic.com/mcp/' }],
    docs: 'https://docs.newrelic.com/docs/agentic-ai/mcp/setup/',
  },
  {
    key: 'honeycomb', name: 'Honeycomb', group: 'Monitoring', what: 'Traces, queries, SLOs',
    url: 'https://mcp.honeycomb.io/mcp', auth: 'oauth',
    variants: [{ label: 'US', url: 'https://mcp.honeycomb.io/mcp' }, { label: 'EU', url: 'https://mcp.eu1.honeycomb.io/mcp' }],
    docs: 'https://docs.honeycomb.io/integrations/mcp/configuration-guide',
  },
  {
    key: 'splunk', name: 'Splunk', group: 'Monitoring', what: 'Searches and saved searches on your Splunk',
    url: 'https://{host}:8089/services/mcp', placeholders: { host: 'Your Splunk host' }, auth: 'bearer', alsoAuth: ['oauth'], keyName: 'encrypted token from the Splunk MCP app',
    docs: 'https://help.splunk.com/en/splunk-cloud-platform/mcp-server-for-splunk-platform/', note: 'Install the MCP Server app from Splunkbase first. Splunk Cloud can turn on sign-in with Splunk (OAuth) for your stack; Splunk Enterprise uses a token.',
  },
  {
    key: 'elastic', name: 'Elastic', group: 'Monitoring', what: 'Search your Elasticsearch data through Agent Builder',
    url: '{kibana}/api/agent_builder/mcp', placeholders: { kibana: 'Your Kibana address (https://…)' }, auth: 'bearer', keyName: 'API key, entered as “ApiKey <key>”',
    docs: 'https://www.elastic.co/docs/explore-analyze/ai-features/agent-builder/mcp-server',
  },
  {
    key: 'dynatrace', name: 'Dynatrace', group: 'Monitoring', what: 'Problems, logs, metrics (DQL)',
    url: 'https://{environment}.apps.dynatrace.com/platform-reserved/mcp-gateway/v0.1/servers/dynatrace-mcp/mcp', placeholders: { environment: 'Your environment ID' },
    auth: 'bearer', keyName: 'platform token', docs: 'https://docs.dynatrace.com/docs/shortlink/dynatrace-mcp-server',
  },
  {
    key: 'cloudflare', name: 'Cloudflare', group: 'Monitoring', what: 'Workers logs and analytics',
    url: 'https://observability.mcp.cloudflare.com/mcp', auth: 'oauth',
    variants: [{ label: 'Observability', url: 'https://observability.mcp.cloudflare.com/mcp' }, { label: 'Documentation', url: 'https://docs.mcp.cloudflare.com/mcp' }],
    docs: 'https://developers.cloudflare.com/agents/model-context-protocol/mcp-servers-for-cloudflare/', check: true,
  },
  {
    key: 'atlassian', name: 'Jira and Confluence', group: 'Work', what: 'Jira issues, Confluence pages, Compass',
    url: 'https://mcp.atlassian.com/v2/mcp', auth: 'oauth', alsoAuth: ['bearer'], keyName: 'API token (an org admin must allow tokens)',
    docs: 'https://developer.atlassian.com/cloud/rovo-mcp/', note: 'Atlassian’s Rovo MCP server. Compare the address with Atlassian’s page.', check: true,
  },
  {
    key: 'notion', name: 'Notion', group: 'Work', what: 'Search and read pages and databases',
    url: 'https://mcp.notion.com/mcp', auth: 'oauth', docs: 'https://developers.notion.com/docs/get-started-with-mcp',
  },
  {
    key: 'linear', name: 'Linear', group: 'Work', what: 'Issues, projects, cycles (read-only address)',
    url: 'https://mcp.linear.app/mcp/readonly', auth: 'oauth', alsoAuth: ['bearer'], keyName: 'API key',
    variants: [{ label: 'Read-only', url: 'https://mcp.linear.app/mcp/readonly' }, { label: 'Full', url: 'https://mcp.linear.app/mcp' }],
    docs: 'https://linear.app/docs/mcp',
  },
  {
    key: 'slack', name: 'Slack', group: 'Work', what: 'Search messages, channels and threads',
    url: 'https://mcp.slack.com/mcp', auth: 'oauth', docs: 'https://docs.slack.dev/ai/mcp-server',
    note: 'Needs a Slack app with MCP turned on: enter its client ID and secret under More options.',
  },
  {
    key: 'github', name: 'GitHub', group: 'Code', what: 'Issues, pull requests, Actions runs, security alerts',
    url: 'https://api.githubcopilot.com/mcp/', auth: 'bearer', keyName: 'fine-grained personal access token (read-only)',
    docs: 'https://github.com/github/github-mcp-server',
  },
  // When the Hub has a GitHub App with its client ID and secret, this entry signs in with it: see githubSignIn.
  {
    key: 'gitlab', name: 'GitLab', group: 'Code', what: 'Issues, merge requests, pipelines',
    url: 'https://gitlab.com/api/v4/mcp', auth: 'oauth', docs: 'https://docs.gitlab.com/user/gitlab_duo/model_context_protocol/mcp_server',
    note: 'Beta, on Premium and Ultimate. For your own GitLab, change the address to https://<your host>/api/v4/mcp.',
  },
  {
    key: 'supabase', name: 'Supabase', group: 'Other', what: 'Tables, logs and read-only SQL',
    url: 'https://mcp.supabase.com/mcp?read_only=true', auth: 'oauth', docs: 'https://github.com/supabase/mcp',
  },
  {
    key: 'stripe', name: 'Stripe', group: 'Other', what: 'Customers, payments, subscriptions',
    url: 'https://mcp.stripe.com', auth: 'oauth', alsoAuth: ['bearer'], keyName: 'restricted API key (read-only)',
    docs: 'https://docs.stripe.com/mcp', note: 'Public preview.',
  },
];

export const CUSTOM_MCP: McpEntry = {
  key: 'custom', name: 'Any MCP server', group: 'Other', what: 'Any server that speaks MCP over HTTP',
  url: '', auth: 'none', alsoAuth: ['oauth', 'bearer', 'header', 'aws', 'google'], docs: 'https://modelcontextprotocol.io',
  note: 'Servers that only run locally (stdio) need an HTTP bridge in front of them.',
};

export const AUTH_LABEL: Record<McpAuth, string> = {
  none: 'No sign-in',
  oauth: 'Sign in with the product (OAuth)',
  bearer: 'A token',
  header: 'A key in a header',
  aws: 'AWS credentials (IAM role or keys)',
  google: 'Google Cloud service account',
};

/**
 * githubSignIn: the GitHub MCP server takes GitHub App user tokens but does not let apps register
 * themselves, so with the Hub's own GitHub App (client ID and secret stored) the admin signs in on GitHub's
 * page instead of pasting a token. A token stays available.
 */
export function githubSignIn(entry: McpEntry, appReady: boolean): McpEntry {
  if (entry.key !== 'github' || !appReady) return entry;
  return {
    ...entry, auth: 'oauth', alsoAuth: ['bearer'],
    note: 'Sign in on GitHub’s own page with the Hub’s GitHub App. Ask then looks things up as you, but only in the repositories the App is installed on and only with the App’s permissions (read issues, pull requests, contents and Actions for the lookups). GitHub renews the sign-in every 8 hours by itself; after 6 months unused, or if you revoke the App, sign in again.',
  };
}

export function mcpEntry(key: string): McpEntry | undefined {
  return key === 'custom' ? CUSTOM_MCP : MCP_CATALOG.find((e) => e.key === key);
}

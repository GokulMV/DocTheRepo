// Signal source catalogue for the "Add signal source" dialog: what each connector type needs and how
// events reach the Hub. Keys match the adapters' config fields.

export interface SourceSpec {
  type: string;
  label: string;
  group: 'Errors & alerts' | 'Cloud logs & alarms' | 'Event platforms';
  modes: ('webhook' | 'poll' | 'both')[];
  config?: { key: string; hint: string; required?: boolean }[];
  credentials?: string; // what goes in the encrypted credentials field
  secret?: boolean; // needs a webhook secret
  help: string;
}

const thresholds = [
  { key: 'lag_min', hint: 'Lag threshold floor (default 1000)' },
  { key: 'oldest_age_seconds', hint: 'Oldest message age that counts as lag (default 300)' },
];

export const SOURCES: SourceSpec[] = [
  { type: 'sentry', label: 'Sentry', group: 'Errors & alerts', modes: ['webhook'], secret: true, help: 'Internal integration webhook; the secret is the client secret Sentry signs with.' },
  { type: 'pagerduty', label: 'PagerDuty', group: 'Errors & alerts', modes: ['webhook'], secret: true, help: 'Webhook v3 subscription; the secret is the subscription signing secret.' },
  { type: 'opsgenie', label: 'Opsgenie', group: 'Errors & alerts', modes: ['webhook'], secret: true, help: 'Webhook integration with the secret as an X-DTH-Token header or ?token= on the URL.' },
  { type: 'datadog', label: 'Datadog', group: 'Errors & alerts', modes: ['webhook'], secret: true, help: 'Webhooks integration; add the secret as a custom header (Authorization: Bearer …).' },
  { type: 'grafana', label: 'Grafana', group: 'Errors & alerts', modes: ['webhook'], secret: true, help: 'Contact point of type webhook with the secret as bearer token or basic-auth password.' },
  { type: 'alertmanager', label: 'Prometheus Alertmanager', group: 'Errors & alerts', modes: ['webhook'], secret: true, help: 'webhook_configs with http_config.authorization.credentials set to the secret.' },
  { type: 'generic', label: 'Generic webhook', group: 'Errors & alerts', modes: ['webhook'], secret: true,
    config: [{ key: 'field.title', hint: 'Dotted path of the title, e.g. alert.name (default: title, name, summary…)' }, { key: 'field.severity', hint: 'Default: severity, level, priority' },
      { key: 'field.external_id', hint: 'Default: id, event_id, uuid' }, { key: 'field.service', hint: 'Default: service, app, component' }, { key: 'source_name', hint: 'Name shown as the source' }],
    help: 'Any tool that can POST JSON. Common field names are found automatically; map others with dotted paths.' },
  { type: 'cloudwatch', label: 'AWS CloudWatch', group: 'Cloud logs & alarms', modes: ['webhook', 'poll', 'both'], secret: true,
    config: [{ key: 'region', hint: 'e.g. eu-west-1', required: true }, { key: 'role_arn', hint: 'Read-only role to assume (cross-account)' }, { key: 'external_id', hint: 'External ID for the role' }, { key: 'log_groups', hint: 'Comma-separated log groups to poll' }, { key: 'filter_pattern', hint: 'Default: ?ERROR ?Exception ?Traceback ?FATAL ?panic' }],
    credentials: 'Optional {"access_key_id","secret_access_key"}; empty uses the Hub’s own AWS identity',
    help: 'Alarms arrive by EventBridge API destination (webhook); log groups without a subscription are polled.' },
  { type: 'firehose', label: 'Amazon Data Firehose (CloudWatch Logs)', group: 'Cloud logs & alarms', modes: ['webhook'], secret: true,
    help: 'HTTP endpoint destination; the secret is the endpoint access key. Use for high-volume log groups (subscription filter → Firehose).' },
  { type: 'gcp', label: 'Google Cloud (Monitoring / Logging)', group: 'Cloud logs & alarms', modes: ['webhook', 'poll', 'both'], secret: true,
    config: [{ key: 'project', hint: 'Project to poll Cloud Logging in' }, { key: 'filter', hint: 'Default: severity>=ERROR' }],
    credentials: 'Optional service-account key JSON; empty uses Workload Identity',
    help: 'Monitoring alerts by webhook channel; Cloud Logging entries polled. For volume use a Log Router sink → Pub/Sub.' },
  { type: 'pubsub', label: 'Google Pub/Sub (log sink)', group: 'Cloud logs & alarms', modes: ['poll'],
    config: [{ key: 'subscription', hint: 'projects/<p>/subscriptions/<s>', required: true }, { key: 'min_severity', hint: 'Default warning' }],
    credentials: 'Optional service-account key JSON', help: 'Pulls a subscription on a Log Router sink topic; acknowledges only after events are saved.' },
  { type: 'kafka', label: 'Kafka / MSK / Confluent', group: 'Event platforms', modes: ['poll'],
    config: [{ key: 'brokers', hint: 'host:9092,host2:9092', required: true }, { key: 'tls', hint: 'true for TLS' }, { key: 'sasl_mechanism', hint: 'plain, scram-sha-256, scram-sha-512' }, { key: 'groups', hint: 'Consumer groups to watch (glob, comma-separated)' }, { key: 'dlq_patterns', hint: 'Default *.dlq,*-dlq,*.DLT…' }, ...thresholds],
    credentials: 'For SASL: {"username","password"}', help: 'Reads lag and dead-letter topics. Never joins or commits for your consumer groups.' },
  { type: 'sqs', label: 'Amazon SQS', group: 'Event platforms', modes: ['poll'],
    config: [{ key: 'region', hint: 'e.g. eu-west-1', required: true }, { key: 'role_arn', hint: 'Read-only role' }, { key: 'external_id', hint: '' }, { key: 'queue_prefix', hint: 'Only queues starting with…' }, { key: 'peek', hint: 'true to sample dead letters (increments receive count)' }, ...thresholds],
    help: 'Queue depth, oldest message age, and dead-letter queues found from redrive policies.' },
  { type: 'sns', label: 'Amazon SNS', group: 'Event platforms', modes: ['poll'], config: [{ key: 'region', hint: '', required: true }, { key: 'role_arn', hint: '' }, { key: 'topics', hint: 'Topic names (comma-separated, * suffix)' }], help: 'Delivery failures per topic (CloudWatch metrics).' },
  { type: 'eventbridge', label: 'Amazon EventBridge', group: 'Event platforms', modes: ['webhook', 'poll'], secret: true, config: [{ key: 'region', hint: '' }, { key: 'role_arn', hint: '' }, { key: 'rules', hint: 'Rule names to watch' }], help: 'Failed rule invocations (poll); events by API destination (webhook).' },
  { type: 'kinesis', label: 'Amazon Kinesis', group: 'Event platforms', modes: ['poll'], config: [{ key: 'region', hint: '', required: true }, { key: 'role_arn', hint: '' }, { key: 'streams', hint: 'Streams to watch' }, ...thresholds], help: 'Iterator age of streams and Lambda consumers.' },
  { type: 'pubsub_bus', label: 'Google Pub/Sub (backlog & DLQ)', group: 'Event platforms', modes: ['poll'], config: [{ key: 'project', hint: '', required: true }, { key: 'subscriptions', hint: 'Subscriptions to watch' }, { key: 'dlq_subscriptions', hint: 'Hub-owned subscriptions on dead-letter topics' }, ...thresholds], credentials: 'Optional service-account key JSON', help: 'Undelivered messages and oldest unacked age; dead letters via Hub-owned subscriptions.' },
  { type: 'rabbitmq', label: 'RabbitMQ', group: 'Event platforms', modes: ['poll'], config: [{ key: 'url', hint: 'Management API, e.g. https://mq:15671', required: true }, { key: 'vhost', hint: '' }, { key: 'peek', hint: 'true to sample dead letters (requeued)' }, ...thresholds], credentials: '{"username","password"} of a monitoring-tagged user', help: 'Ready backlog and queues bound to dead-letter exchanges.' },
];

export const sourceSpec = (type: string) => SOURCES.find((s) => s.type === type);

// Knowledge sources (Confluence, Jira): synced read-only every 15 minutes for Q&A, decode runbooks, the
// Library, and known-issue import.
export interface KnowledgeSpec {
  type: 'confluence' | 'jira';
  label: string;
  config: { key: string; label: string; hint: string; required?: boolean; placeholder?: string }[];
  help: string;
}

const labelField = { key: 'known_issue_label', label: 'Known-issue label', hint: 'Issues/pages with this label become draft known-issue rules (default known-issue; "-" turns import off).' };

export const KNOWLEDGE: KnowledgeSpec[] = [
  { type: 'confluence', label: 'Confluence', help: 'Pages in the listed spaces are synced (changed pages every 15 minutes, deletions daily). Use a read-only account.',
    config: [
      { key: 'base_url', label: 'Site URL', hint: 'Cloud: https://<site>.atlassian.net/wiki · Data Center: https://confluence.example.com', required: true, placeholder: 'https://acme.atlassian.net/wiki' },
      { key: 'spaces', label: 'Spaces', hint: 'Space keys, comma-separated', required: true, placeholder: 'ENG, OPS' },
      { key: 'email', label: 'Account e-mail', hint: 'Cloud only (with an API token). Leave empty for a Data Center personal access token.' },
      { key: 'timezone', label: 'Time zone', hint: 'The account’s Confluence time zone (IANA, e.g. Europe/Berlin); speeds up incremental sync.' },
      labelField,
    ] },
  { type: 'jira', label: 'Jira', help: 'Issues in the listed projects are synced. A labelled issue moving to Done turns its rule from suppress to label only.',
    config: [
      { key: 'base_url', label: 'Site URL', hint: 'Cloud: https://<site>.atlassian.net · Data Center: https://jira.example.com', required: true, placeholder: 'https://acme.atlassian.net' },
      { key: 'projects', label: 'Projects', hint: 'Project keys, comma-separated', required: true, placeholder: 'ENG, OPS' },
      { key: 'email', label: 'Account e-mail', hint: 'Cloud only (with an API token). Leave empty for a Data Center personal access token.' },
      { key: 'jql', label: 'Extra JQL filter', hint: 'ANDed into every query, e.g. issuetype in (Bug, Incident)' },
      { key: 'lookback_days', label: 'History on first sync (days)', hint: 'Default 365' },
      labelField,
    ] },
];

export const knowledgeSpec = (type: string) => KNOWLEDGE.find((k) => k.type === type);

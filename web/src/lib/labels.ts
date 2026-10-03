// Display labels. Values sent to the API stay as they are (lowercase identifiers); people see these.

/** sentence turns an identifier or lowercase phrase into a label: "spend_blocked" → "Spend blocked". */
export function sentence(s: string): string {
  const t = s.replace(/_/g, ' ');
  return t.charAt(0).toUpperCase() + t.slice(1);
}

/** capFirst capitalizes the first letter and leaves the rest alone (names, phrases with identifiers). */
export function capFirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** What each routed feature is called in the UI. */
export const FEATURE_LABELS: Record<string, string> = {
  docgen: 'Docs generation',
  docgen_fast: 'Docs (short code)',
  qa: 'Ask (Q&A)',
  decode: 'Error explanations',
  triage: 'Change triage',
  embedding: 'Embeddings',
  suggest: 'Known-issue suggestions',
  decide: 'Decisions',
  security: 'Security scans',
  sift: 'Ask source picking',
};

export const featureLabel = (f: string) => FEATURE_LABELS[f] ?? sentence(f);

/** Job types, as people see them. */
export const JOB_LABELS: Record<string, string> = {
  code_push: 'Docs update',
  decode_issue: 'Error explanation',
  knowledge_sync: 'Knowledge sync',
  security_scan: 'Security scan',
  security_fix: 'Security fix',
  import_docs: 'Docs import',
  reindex: 'Reindex',
  signal_batch: 'Signal batch',
  pr_review: 'PR review',
};
export const jobLabel = (t: string) => JOB_LABELS[t] ?? sentence(t);

/** How docs land in a repository. */
export const PUSH_MODE_LABELS: Record<string, string> = {
  pr_auto_merge: 'Auto-merged PR',
  pr_with_approver: 'PR for an approver',
  direct: 'Direct commit',
};
export const pushModeLabel = (m: string) => PUSH_MODE_LABELS[m] ?? sentence(m);

const NAMES: Record<string, string> = {
  github: 'GitHub', gitlab: 'GitLab', sentry: 'Sentry', pagerduty: 'PagerDuty', opsgenie: 'Opsgenie', datadog: 'Datadog',
  grafana: 'Grafana', alertmanager: 'Alertmanager', generic: 'Generic webhook', wiz: 'Wiz', splunk: 'Splunk',
  cloudwatch: 'AWS CloudWatch', firehose: 'Amazon Data Firehose', gcp: 'Google Cloud', pubsub: 'Google Pub/Sub', kafka: 'Kafka',
  sqs: 'Amazon SQS', sns: 'Amazon SNS', eventbridge: 'Amazon EventBridge', kinesis: 'Amazon Kinesis', pubsub_bus: 'Google Pub/Sub (backlog)',
  rabbitmq: 'RabbitMQ', confluence: 'Confluence', jira: 'Jira', notion: 'Notion', upload: 'Uploaded',
  anthropic: 'Anthropic', openai: 'OpenAI', azure_openai: 'Azure OpenAI', bedrock: 'AWS Bedrock', vertex: 'Google Vertex AI',
  ollama: 'Ollama', openai_compat: 'OpenAI-compatible', external_cli: 'Agent CLI', opencode: 'opencode', github_models: 'GitHub Models', jev: 'TypeSafe Jev',
  webhook: 'Webhook', poll: 'Polling', both: 'Webhook and polling',
};
/** Connector, provider and mode identifiers as product names ("openai_compat" → "OpenAI-compatible"). */
export const nameOf = (id: string) => NAMES[id] ?? sentence(id);

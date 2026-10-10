# Connector setup

Every connector is added under **Connectors** in the Hub (admins), or in a settings file
([settings-file.md](settings-file.md)). Credentials are sealed in your browser and stored encrypted.
They are write-only: you can replace them, never read them back.

- **Webhook connectors:** after you add one, the Hub shows the webhook URL
  (`<public URL>/hooks/<type>/<connector id>`) and, once, the secret to paste into the other tool.
- **Polling connectors:** they run on the scheduler, every minute for git hosts and every 5 minutes for
  most signal sources. They read only, and save their position after events are stored, so nothing is
  lost on restart.

## Git hosts

| Host | Easiest | Alternatives | Details |
|---|---|---|---|
| GitHub (cloud or Enterprise Server) | **Connect with GitHub**: one click creates a private GitHub App and installs it on the repositories you pick | Use an existing GitHub App (App ID + private key), or a fine-grained token with Contents and Pull requests read & write, Metadata read | [github.md](github.md) |
| GitLab (gitlab.com or self-managed) | A token with the `api` scope (project, group or personal access token); set `base_url` for self-managed (`https://gitlab.example.com/api/v4/`) | | [github.md](github.md#by-hand) |

Webhooks are registered for you when the Hub's address is reachable from the git host. Otherwise it polls.
Docs land as an auto-merged PR (default), directly, or as a PR for an approver, per repository.

## Knowledge sources

| Source | Needs | Details |
|---|---|---|
| Confluence (Cloud or Data Center) | Cloud: **Connect with Atlassian** (approve on Atlassian's page; one-time app setup by an admin) and space keys. Or the site URL, space keys, and a read-only account: e-mail + API token (Cloud) or a personal access token (Data Center) | [confluence-jira.md](confluence-jira.md) |
| Jira (Cloud or Data Center) | The same choices, with project keys; optional extra JQL | [confluence-jira.md](confluence-jira.md) |

## Signal sources

Errors, alerts and findings for the Inbox. Any source can be marked **never send to a model**, so its
events are grouped and matched but never explained by a model (on by default for Wiz). Wiz and Splunk have
their own guide: [wiz-splunk.md](wiz-splunk.md).

### Errors & alerts

#### Sentry (`sentry`)

Internal integration webhook; the secret is the client secret Sentry signs with.

- **How events arrive:** webhook; the webhook secret is shown once when you add it.

#### PagerDuty (`pagerduty`)

Webhook v3 subscription; the secret is the subscription signing secret.

- **How events arrive:** webhook; the webhook secret is shown once when you add it.

#### Opsgenie (`opsgenie`)

Webhook integration with the secret as an X-DTH-Token header or ?token= on the URL.

- **How events arrive:** webhook; the webhook secret is shown once when you add it.

#### Datadog (`datadog`)

Webhooks integration; add the secret as a custom header (Authorization: Bearer …).

- **How events arrive:** webhook; the webhook secret is shown once when you add it.

#### Grafana (`grafana`)

Contact point of type webhook with the secret as bearer token or basic-auth password.

- **How events arrive:** webhook; the webhook secret is shown once when you add it.

#### Prometheus Alertmanager (`alertmanager`)

webhook_configs with http_config.authorization.credentials set to the secret.

- **How events arrive:** webhook; the webhook secret is shown once when you add it.

#### Generic webhook (`generic`)

Any tool that can POST JSON. Common field names are found automatically; map others with dotted paths.

- **How events arrive:** webhook; the webhook secret is shown once when you add it.
- **Settings:**
  - `field.title`: Dotted path of the title, e.g. alert.name (default: title, name, summary…)
  - `field.severity`: Default: severity, level, priority
  - `field.external_id`: Default: id, event_id, uuid
  - `field.service`: Default: service, app, component
  - `source_name`: Name shown as the source

### Security & log platforms

#### Wiz (`wiz`)

Security issues as findings (one per control and resource). Webhook: a Wiz webhook integration with the secret as bearer token; or poll the GraphQL API every 5 minutes.

- **How events arrive:** webhook, poll, webhook + poll; the webhook secret is shown once when you add it.
- **Credentials:** Polling: {"client_id","client_secret"} of a service account with read:issues.
- **Settings:**
  - `api_url`: Polling: tenant GraphQL endpoint, e.g. https://api.us17.app.wiz.io/graphql
  - `auth_url`: Default https://auth.app.wiz.io/oauth/token
  - `statuses`: Default OPEN,IN_PROGRESS
  - `severities`: e.g. CRITICAL,HIGH (default all)
  - `service_tag`: Resource tag holding the service (default service)

#### Splunk (`splunk`)

Webhook alert action: add ?token=<secret> to the URL (Splunk cannot send headers). Polling runs saved searches or SPL over each window; every result row becomes an event.

- **How events arrive:** webhook, poll, webhook + poll; the webhook secret is shown once when you add it.
- **Credentials:** Polling: an authentication token, or {"username","password"}.
- **Settings:**
  - `base_url`: Polling: REST API, e.g. https://splunk.example.com:8089
  - `saved_searches`: Saved searches to run (comma-separated)
  - `queries`: SPL queries to run (one per line)
  - `app`: Namespace (default search)
  - `lag_seconds`: Indexing delay to leave out (default 60)
  - `field.service`: Result field holding the service (default service, service_name, app_name…)

### Cloud logs & alarms

#### AWS CloudWatch (`cloudwatch`)

Alarms arrive by EventBridge API destination (webhook); log groups without a subscription are polled.

- **How events arrive:** webhook, poll, webhook + poll; the webhook secret is shown once when you add it.
- **Credentials:** Optional {"access_key_id","secret_access_key"}; empty uses the Hub’s own AWS identity.
- **Settings:**
  - `region` (required): e.g. eu-west-1
  - `role_arn`: Read-only role to assume (cross-account)
  - `external_id`: External ID for the role
  - `log_groups`: Comma-separated log groups to poll
  - `filter_pattern`: Default: ?ERROR ?Exception ?Traceback ?FATAL ?panic

#### Amazon Data Firehose (CloudWatch Logs) (`firehose`)

HTTP endpoint destination; the secret is the endpoint access key. Use for high-volume log groups (subscription filter → Firehose).

- **How events arrive:** webhook; the webhook secret is shown once when you add it.

#### Google Cloud (Monitoring / Logging) (`gcp`)

Monitoring alerts by webhook channel; Cloud Logging entries polled. For volume use a Log Router sink → Pub/Sub.

- **How events arrive:** webhook, poll, webhook + poll; the webhook secret is shown once when you add it.
- **Credentials:** Optional service-account key JSON; empty uses Workload Identity.
- **Settings:**
  - `project`: Project to poll Cloud Logging in
  - `filter`: Default: severity>=ERROR

#### Google Pub/Sub (log sink) (`pubsub`)

Pulls a subscription on a Log Router sink topic; acknowledges only after events are saved.

- **How events arrive:** poll.
- **Credentials:** Optional service-account key JSON.
- **Settings:**
  - `subscription` (required): projects/<p>/subscriptions/<s>
  - `min_severity`: Default warning

### Event platforms

#### Kafka / MSK / Confluent (`kafka`)

Reads lag and dead-letter topics. Never joins or commits for your consumer groups.

- **How events arrive:** poll.
- **Credentials:** For SASL: {"username","password"}.
- **Settings:**
  - `brokers` (required): host:9092,host2:9092
  - `tls`: true for TLS
  - `sasl_mechanism`: plain, scram-sha-256, scram-sha-512
  - `groups`: Consumer groups to watch (glob, comma-separated)
  - `dlq_patterns`: Default *.dlq,*-dlq,*.DLT…
  - `lag_min`: Lag threshold floor (default 1000)
  - `oldest_age_seconds`: Oldest message age that counts as lag (default 300)

#### Amazon SQS (`sqs`)

Queue depth, oldest message age, and dead-letter queues found from redrive policies.

- **How events arrive:** poll.
- **Settings:**
  - `region` (required): e.g. eu-west-1
  - `role_arn`: Read-only role
  - `external_id`
  - `queue_prefix`: Only queues starting with…
  - `peek`: true to sample dead letters (increments receive count)
  - `lag_min`: Lag threshold floor (default 1000)
  - `oldest_age_seconds`: Oldest message age that counts as lag (default 300)

#### Amazon SNS (`sns`)

Delivery failures per topic (CloudWatch metrics).

- **How events arrive:** poll.
- **Settings:**
  - `region` (required)
  - `role_arn`
  - `topics`: Topic names (comma-separated, * suffix)

#### Amazon EventBridge (`eventbridge`)

Failed rule invocations (poll); events by API destination (webhook).

- **How events arrive:** webhook, poll; the webhook secret is shown once when you add it.
- **Settings:**
  - `region`
  - `role_arn`
  - `rules`: Rule names to watch

#### Amazon Kinesis (`kinesis`)

Iterator age of streams and Lambda consumers.

- **How events arrive:** poll.
- **Settings:**
  - `region` (required)
  - `role_arn`
  - `streams`: Streams to watch
  - `lag_min`: Lag threshold floor (default 1000)
  - `oldest_age_seconds`: Oldest message age that counts as lag (default 300)

#### Google Pub/Sub (backlog & DLQ) (`pubsub_bus`)

Undelivered messages and oldest unacked age; dead letters via Hub-owned subscriptions.

- **How events arrive:** poll.
- **Credentials:** Optional service-account key JSON.
- **Settings:**
  - `project` (required)
  - `subscriptions`: Subscriptions to watch
  - `dlq_subscriptions`: Hub-owned subscriptions on dead-letter topics
  - `lag_min`: Lag threshold floor (default 1000)
  - `oldest_age_seconds`: Oldest message age that counts as lag (default 300)

#### RabbitMQ (`rabbitmq`)

Ready backlog and queues bound to dead-letter exchanges.

- **How events arrive:** poll.
- **Credentials:** {"username","password"} of a monitoring-tagged user.
- **Settings:**
  - `url` (required): Management API, e.g. https://mq:15671
  - `vhost`
  - `peek`: true to sample dead letters (requeued)
  - `lag_min`: Lag threshold floor (default 1000)
  - `oldest_age_seconds`: Oldest message age that counts as lag (default 300)


## Model providers

These are not connectors, but they are set up the same way, under **Providers & routing**. The form for
each provider links to where its key is created: Anthropic, OpenAI, Azure OpenAI, AWS Bedrock, Google
Vertex AI, Ollama, any OpenAI-compatible server, an agent CLI, or TypeSafe Jev ([jev.md](jev.md)).
Routing then chooses which model serves each feature: docs, answers, error explanations, triage and
embeddings.

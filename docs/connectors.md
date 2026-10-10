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
| Confluence (Cloud or Data Center) | Site URL, space keys, and a read-only account: e-mail + API token (Cloud) or a personal access token (Data Center) | [confluence-jira.md](confluence-jira.md) |
| Jira (Cloud or Data Center) | Site URL, project keys, the same kind of account; optional extra JQL | [confluence-jira.md](confluence-jira.md) |

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
- **Credentials:** Optional {"access_key_id","secret_access_key"}; empty uses the Hub’s own AWS identity (or the [read-only role](#read-only-role-in-aws)).
- **Settings:**
  - `region` (required): e.g. eu-west-1
  - `role_arn`: Filled in by “Create a read-only role in AWS” ([read-only role](#read-only-role-in-aws)), or a role of your own
  - `external_id`: External ID the role requires
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
  - `role_arn`: Filled in by “Create a read-only role in AWS”, or a role of your own
  - `external_id`: External ID the role requires
  - `queue_prefix`: Only queues starting with…
  - `peek`: true to sample dead letters (increments receive count)
  - `lag_min`: Lag threshold floor (default 1000)
  - `oldest_age_seconds`: Oldest message age that counts as lag (default 300)

#### Amazon SNS (`sns`)

Delivery failures per topic (CloudWatch metrics).

- **How events arrive:** poll.
- **Settings:**
  - `region` (required)
  - `role_arn`: Filled in by “Create a read-only role in AWS”, or a role of your own
  - `external_id`: External ID the role requires
  - `topics`: Topic names (comma-separated, * suffix)

#### Amazon EventBridge (`eventbridge`)

Failed rule invocations (poll); events by API destination (webhook).

- **How events arrive:** webhook, poll; the webhook secret is shown once when you add it.
- **Settings:**
  - `region`
  - `role_arn`: Filled in by “Create a read-only role in AWS”, or a role of your own
  - `external_id`: External ID the role requires
  - `rules`: Rule names to watch

#### Amazon Kinesis (`kinesis`)

Iterator age of streams and Lambda consumers.

- **How events arrive:** poll.
- **Settings:**
  - `region` (required)
  - `role_arn`: Filled in by “Create a read-only role in AWS”, or a role of your own
  - `external_id`: External ID the role requires
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


## Read-only role in AWS

The AWS signal sources (CloudWatch, SQS, SNS, EventBridge, Kinesis) and the AWS MCP connection can read
another AWS account without access keys. AWS creates a read-only IAM role there that trusts only the Hub.

1. In the connector's form (or the AWS MCP connection under **AWS credentials → A read-only role in your
   AWS account**), click **Create a read-only role in AWS**.
2. The AWS console opens on **Quick create stack** with everything filled in. Tick the IAM acknowledgement
   and click **Create stack**. Without a quick-create link (see [Hosting the template](#hosting-the-template)),
   the Hub offers the template file and the exact command instead:
   `aws cloudformation deploy --stack-name doctherepo-hub-readonly-… --template-file doctherepo-hub-readonly-….yaml --capabilities CAPABILITY_NAMED_IAM --parameter-overrides …`.
   You can also upload that file under **CloudFormation → Create stack → Upload a template file**.
3. Enter the **AWS account ID** (or paste the stack's `RoleArn` output) and click **Check access**.

The Hub works out the role ARN from the role's fixed name (`DocTheRepoHubReadOnly-<10 characters>`),
assumes it with the External ID, and says exactly what is wrong if it can't:

| Check says | Meaning |
|---|---|
| no role it may assume | The stack is not finished yet (wait for `CREATE_COMPLETE`), or the account ID is wrong. STS reports a missing role as "access denied", so the Hub cannot tell these apart |
| trust policy does not let the Hub in | The role exists but trusts another principal or External ID. Create it from this connection's link or template |
| the Hub's own IAM principal is not allowed | Give the Hub's role a policy allowing `sts:AssumeRole` on `arn:aws:iam::*:role/DocTheRepoHubReadOnly-*` |
| can be assumed without the External ID | The trust policy lacks the `sts:ExternalId` condition. The Hub refuses such a role |
| missing permission … | The role works but lacks a call this connector makes |

On success the form fills in `role_arn` and `external_id`. You can still type a role ARN and External ID of
your own, or use access keys. The Hub renews the role's session (one hour) before it expires.

**What the role may do.** You choose this when you create the role:

- **Only what the Hub reads** (default for signal sources): an inline policy with exactly
  `cloudwatch:DescribeAlarmHistory`, `cloudwatch:GetMetricData`, `cloudwatch:ListMetrics`,
  `logs:FilterLogEvents`, `sqs:GetQueueAttributes` and `sqs:ListQueues`, on all resources. SQS **peek**
  also needs `sqs:ReceiveMessage`. That call hides a message for a moment and counts as a receive, so it is
  not included: add it to the role yourself if you turn peek on.
- **Read-only access to everything** (default for the AWS MCP connection, which can ask about any service):
  the AWS managed policy `ReadOnlyAccess`. It can read data too, such as S3 objects. Use it only if Ask may
  see that.

The trust policy allows `sts:AssumeRole` for the Hub's IAM principal only, with the condition
`StringEquals {"sts:ExternalId": "<this connection's ID>"}`. Nothing in the template can write or change
anything in your account.

**Why an External ID.** It guards against the "confused deputy" problem. The Hub makes a new random ID
(`dth-` and 160 random bits) for every connection, and the role accepts only that ID. The ID is not a
secret, but knowing your role's ARN is not enough to make the Hub use your role: the caller also needs
the ID the Hub picked for your connection.

**Where it does not work.** The Hub needs an AWS identity of its own, the role the trust policy names: an ECS
task role, an EKS pod role, an EC2 instance profile, or an IAM user's keys in its environment. A Hub on a
laptop or outside AWS has none. The button then says so, and you use access keys or, for the AWS MCP server,
**Sign in with AWS** in the browser. A Hub using the account's root credentials is refused. The Hub's role
also needs `sts:AssumeRole` on `arn:aws:iam::*:role/DocTheRepoHubReadOnly-*` in its own policy.

### Hosting the template

The AWS console's quick-create links only take a template stored in Amazon S3. To get the one-click link:

1. Download the generic template: `GET /api/v1/aws/role/template` as an admin. Without `external_id`, it has
   no connection's values; the link passes them.
2. Upload it to an S3 bucket the people creating roles can read, e.g.
   `aws s3 cp doctherepo-hub-readonly.yaml s3://<bucket>/doctherepo-hub-readonly.yaml`.
3. Set `aws.role_template_url` (`DTH_AWS_ROLE_TEMPLATE_URL`) to its https URL, e.g.
   `https://<bucket>.s3.<region>.amazonaws.com/doctherepo-hub-readonly.yaml`.

The link has the form
`https://<region>.console.aws.amazon.com/cloudformation/home?region=<region>#/stacks/create/review?templateURL=…&stackName=…&param_HubPrincipalArn=…&param_ExternalId=…&param_RoleName=…&param_AccessLevel=…`.
It is offered only in the standard `aws` partition. If the Hub's role has a path (`role/ops/hub`), set
`aws.hub_principal_arn` (`DTH_AWS_HUB_PRINCIPAL_ARN`) to its full ARN. STS shows the session without the
path.

## Model providers

These are not connectors, but they are set up the same way, under **Providers & routing**. The form for
each provider links to where its key is created: Anthropic, OpenAI, Azure OpenAI, AWS Bedrock, Google
Vertex AI, Ollama, any OpenAI-compatible server, an agent CLI, or TypeSafe Jev ([jev.md](jev.md)).
Routing then chooses which model serves each feature: docs, answers, error explanations, triage and
embeddings.

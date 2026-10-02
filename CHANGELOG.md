# Changelog

Notable changes, newest first. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/) from the first release.

## [Unreleased]

The first public version. Everything below is new.

### Documentation and knowledge
- Living docs on every push: syntax-tree triage, surgical regeneration, three landing modes (direct,
  auto-merged PR, PR with approver), hand-written blocks preserved.
- Ask: hybrid retrieval over code, docs, Confluence and Jira, with citations, per-user history and access
  scoping.
- Palace knowledge graph; Architecture tab with a generated diagram per repository and authored archify
  diagrams.
- Confluence and Jira sync; Library shelves; opencode doc engine and an MCP server (`dth mcp`).

### Error intelligence
- Inbox fed by:
  - Error and alerting tools: Sentry, PagerDuty, Opsgenie, Datadog, Grafana and Alertmanager.
  - Cloud logs and alarms: CloudWatch, Firehose, GCP logging and Pub/Sub.
  - Security and log platforms: Wiz and Splunk.
  - Event platforms: lag and dead letters from Kafka, SQS, SNS, EventBridge, Kinesis and RabbitMQ.
- Scrubbing and PII removal, fingerprint grouping, known-issue rules, decoding with code context, and a
  decision gate (Jev optional).
- Per-source never-send-to-a-model.

### Operations and security
- Bring your own model: Claude (Anthropic API, Bedrock, Vertex), OpenAI, Azure OpenAI, Ollama, any
  OpenAI-compatible server, and external agent CLIs. Spend limits are checked before every paid call.
- One-click **Connect with GitHub** (GitHub App manifest flow). Disabling a GitHub App connector suspends
  the installation on GitHub, and removing it uninstalls the App.
- **Sealed secrets**: X25519 + ML-KEM-768 sealing in the browser and CLI, AES-256-GCM envelopes under a
  local key or AWS/GCP KMS, write-only.
- Settings as code: `dth apply -f` with secret references (env, file, Vault, gopass, AWS/GCP secret
  managers).
- OIDC, RBAC and per-repository access, and an audit log. Roles api/worker/scheduler; Postgres job queue;
  Prometheus metrics.
- Deployment: one-command quickstart, Docker Compose (`dth up`), Helm, and Terraform for AWS and GCP.
- CI gates: govulncheck, npm audit and Trivy; CycloneDX SBOMs.

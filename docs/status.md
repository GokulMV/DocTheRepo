# Status: what is built, phase by phase

Under construction, milestone by milestone (plan § 14). Implemented so far:

- **Phase 1 — Foundation**: configuration with complete defaults, PostgreSQL schema and migrations,
  Postgres job queue (exactly-once claims, per-repo ordering, retries, dead letter, lease recovery),
  worker pool, leader-elected scheduler, envelope encryption for secrets, health/readiness endpoints,
  structured logging and Prometheus metrics.
- **Phase 2 — Core code intelligence**: Tree-sitter grammar registry (Go, Java, Python, TypeScript/TSX,
  JavaScript, Rust built in; more loaded at runtime from shared libraries), per-file syntax analysis,
  AST-diff triage (cosmetic vs structural, dependency major-bump rule, rename detection), stable chunking of
  code and Markdown, manifest diff, surgical doc-file assembly preserving hand-written blocks, scoped
  context with a token budget, multi-scope spend guard, knowledge-graph (Palace) extraction, and Library
  shelf rules.
- **Phase 3 — LLM & embeddings (bring your own keys)**: Claude via the official Anthropic SDK (Claude API,
  Bedrock, Vertex) with server-side refusal fallbacks; OpenAI, Azure OpenAI, and any OpenAI-compatible
  server (Ollama, vLLM, LiteLLM); Bedrock Converse and Vertex Gemini for other model families; embeddings
  on OpenAI-protocol, Bedrock, and Vertex; external agent CLIs through the versioned DocGen contract with
  a conformance suite; an LLM gateway that enforces the spend guard on every call, falls back on
  transient failures, repairs invalid JSON once, and records usage; encrypted provider keys; parity
  suites across all adapters.
- **Phase 4 — Git hosts, ingest, push, pipeline**: GitHub (token or App) and GitLab (SaaS or
  self-managed) adapters with a shared parity suite; webhook ingress at `/hooks/{github,gitlab}/{id}`
  with signature checks, a bot-loop guard, and per-connector rate limits; polling for hosts that cannot
  reach the Hub; the `code_push` pipeline end to end (triage → chunk → scoped context → docgen → doc
  assembly → landing → index → knowledge graph → docs tree); three push modes (`pr_auto_merge` default,
  `direct` with fallback, `pr_with_approver`, where the approver reviews under their own account) and a PR
  lifecycle (merge when green, rebase or regenerate on conflict, close stale PRs, supersede older ones);
  pgvector (default) and Qdrant vector indexes with zero-downtime reindex; Markdown import.
- **Phase 5 — API, auth, and Ask**: Okta/Google/Entra/Keycloak single sign-on (OIDC with PKCE) or a
  local owner account, sessions with CSRF protection, personal access tokens, four roles, per-repo
  access (direct or via IdP groups), and an audit log; Ask with hybrid retrieval (vectors + full text +
  knowledge-graph neighbours), citations that can only point at supplied sources, streaming answers,
  threads, and an answer cache; the docs Tree, Palace, and Library APIs; repository, connector, BYO LLM
  provider, routing, and spend-ceiling administration; jobs, activity, and usage/savings analytics;
  OpenAPI 3.1 at `/api/v1/openapi.json`.
- **Phase 6 — Web UI**: React + TypeScript app embedded in the hub binary — sign-in (SSO or local), Ask
  with streaming answers and citations, Docs tree, Palace graph explorer, Library, Repositories (settings,
  dry run, import), Connectors, Providers & routing, Spend limits, Analytics, Activity & jobs, Users &
  access, personal access tokens, and a setup checklist. Strict CSP, sanitized Markdown, Mermaid diagrams.
- **Phase 7 — CLI & packaging**: the `dth` CLI (one-command local install, ask, repos, import, dry run,
  jobs and retries, usage, tokens, reindex, adapter conformance test, migrations); a distroless container
  image; Docker Compose for local use; Terraform for AWS (ECS Fargate, RDS, ALB, KMS, Secrets Manager) and
  GCP (Cloud Run, Cloud SQL, load balancer, Cloud KMS, Secret Manager); least-privilege read-only roles for
  watched AWS accounts and GCP projects; a Helm chart for any Kubernetes cluster.
- **Phase 8 — End-to-end, load, and quality**: Playwright E2E against the real hub and mocks (cold start
  through the UI to a cited answer; RBAC; spend blocking); k6 push-burst and Q&A load runs; a 50-question
  retrieval eval that gates the build. Measured results: [`docs/perf-results.md`](docs/perf-results.md).
- **Phase 9 — Signal core** (Milestone 2): one normalized event model for errors, alerts, security
  findings, and event-bus conditions; secret scrubbing that is always on (and now applied to every LLM
  call) and per-provider PII redaction; fingerprinting that groups the same error across IDs, customers,
  deploys, and ingestion paths; a known-issue matcher that suppresses before any model call; a per-replica
  aggregation hot path (one database write per distinct error per second, bounded samples,
  backpressure instead of loss); issues, samples, counts, decodes, and known-issue tables with retention.
- **Phase 10 — Signal ingress**: signed or token-authenticated webhooks for Sentry,
  PagerDuty, Opsgenie, Datadog, Grafana, Alertmanager, EventBridge (CloudWatch alarms, GuardDuty), Cloud
  Monitoring, and a generic field-mapping receiver, with a fixture parity suite. Stream receivers
  acknowledge only after events are persisted: an Amazon Data Firehose endpoint (CloudWatch Logs
  subscriptions) and a Pub/Sub pull consumer for Cloud Logging sinks. Log lines are read as JSON or as
  Python, Java, Node.js, and Go stack traces. Pollers cover installs without streams (CloudWatch Logs
  and alarm history with cross-account AssumeRole; Cloud Logging), committing cursors only after events
  are persisted. Read-only event-platform inspectors (Kafka/MSK/Confluent, SQS, SNS, EventBridge,
  Kinesis, Pub/Sub, RabbitMQ) open issues for sustained consumer lag (auto-resolving when it clears) and
  for dead-letter growth, one issue per error class; the Hub never commits offsets or acknowledges an
  application's messages.
- **Phase 11 — Decode & suggestions**: every new error group is explained once, grounded in the code its
  stack trace points at (exact file and symbol, then semantic search in the service's repositories), that
  code's docs, commits from the last 7 days, similar issues explained before, and runbooks, within a 12k
  token budget. A regression whose blamed code is unchanged reuses its decode instead of paying again.
  Known-issue suggestions come from decodes that call an issue noise (no extra model call) or from pasted
  incident text (one call, which may only reference real issues), and a dry run shows what a rule would
  match; nothing is suppressed until a person accepts it.
- **Phase 11.5 — Decisions behind confidence gates**: an optional `decide` model route answers typed
  questions with a probability per option. Before a full decode, the Hub asks whether a new issue is
  recurring noise; only "known noise" at p ≥ `decide.gate_threshold` (default 0.9) replaces the decode with
  a short note (the issue stays visible, and a full decode is one click away). Any chat provider can
  serve the route (probabilities self-reported and labelled so); a native decision model such as TypeSafe
  Jev plugs in by implementing `ports.Decider`, which the gateway then calls directly with the same
  scrubbing, spend guard, and ledger. `go test ./cmd/hub -run TestDecisionEval` scores a provider on
  100 labelled issues: accuracy, calibration (ECE, reliability curve, Brier), gate coverage, defects the
  gate would skip, and net tokens saved per issue. Jev is supported as provider kind `jev`
  ([`docs/jev.md`](docs/jev.md)).
- **Phase 12 — Inbox UI and API**: the Inbox (filters, sparklines, multi-select "mark as known"), issue
  pages (explanation with blamed code and recent commits, events with stacks, hourly chart, actions),
  Known issues (rules, a dry-run tester, suggestions to accept or reject, propose-from-text), signal
  sources on the Connectors page (webhook URL and secret shown once), and savings by kind on Analytics.
  Playwright covers the error flow (Sentry webhook → explained → mark as known → next one suppressed);
  the signal-storm k6 run checks zero lost events at 2,000 events/s
  ([`docs/perf-results.md`](docs/perf-results.md)).
- **Phase 13 — Confluence & Jira**: read-only sync of Confluence spaces and Jira projects (Cloud and Data
  Center) every 15 minutes: pages and issues are converted to Markdown, chunked, embedded, linked in the
  Palace to the services, repositories, and endpoints they mention (`documented_in`, `runbook_for`), and
  shelved in the Library; deletions are reconciled daily. Issues/pages labelled `known-issue` become draft
  rules with a proposed match; a Jira issue moving to Done flips its rule to label only ("fixed upstream —
  verify"). Known Issues explains a pasted Jira or Confluence URL (`POST /known-issues/from-link`). See
  [`docs/confluence-jira.md`](docs/confluence-jira.md).
- **Phase 14 — Wiz & Splunk**: Wiz issues as security findings (webhook integration or GraphQL polling,
  one issue per rule and resource) and Splunk (webhook alert actions, or polling saved searches/SPL into
  log events with stacks), with severity mapping, fixtures in the adapter parity suite, Inbox source and
  kind filters, and a per-connector **never send to a model** policy (on by default for Wiz). See
  [`docs/wiz-splunk.md`](docs/wiz-splunk.md).
- **opencode & MCP**: opencode as a documentation engine (`dth engine opencode`) and the Hub as an MCP
  server for opencode, Claude Code, and Cursor (`dth mcp`). See [`docs/opencode.md`](docs/opencode.md).
- **Architecture tab**: every repository's architecture, generated from its code on each push (callers,
  endpoints, service and modules, topics, datastores, dependencies and consumers), plus diagrams authored
  with [archify](https://github.com/tt-a1i/archify) and committed to the repository, synced on push and shown
  sandboxed. See [`docs/architecture-tab.md`](docs/architecture-tab.md).
- **Settings file**: configure providers, routing, connectors, repositories and spend limits from YAML/JSON
  with secret references (`${env:}`, `${file:…#key}`, `${vault:}`, `${gopass:}`, `${awssm:}`, `${gcpsm:}`):
  `dth apply -f`, or paste it under Administration → Settings file. See
  [`docs/settings-file.md`](docs/settings-file.md).
- **Connect with GitHub**: one click creates a private GitHub App for the Hub (manifest flow), installs it on
  the repositories you choose, and tracks them, with nothing to copy. See [`docs/github.md`](docs/github.md).
- **Sealed secrets**: provider keys and connector credentials are sealed in the browser (or `dth apply`) to the
  Hub's hybrid X25519 + ML-KEM-768 key, stored with per-secret AES-256-GCM under a local or KMS key, and are
  write-only (only a hint is ever shown). See [`docs/security.md`](docs/security.md).
- **Phase 15 — hardening**: govulncheck, npm audit and Trivy gates in CI plus a weekly scan; CycloneDX
  SBOMs (`make sbom`); an operations guide with monitoring, backup/restore (database + master-key custody),
  upgrade and troubleshooting runbooks ([`docs/operations.md`](operations.md)); Prometheus alert rules
  ([`deploy/monitoring/alerts.yaml`](../deploy/monitoring/alerts.yaml)) with model-call, token and latency
  metrics; per-connector setup ([`docs/connectors.md`](connectors.md)); load results
  ([`docs/perf-results.md`](perf-results.md)); and a break-glass `dth-hub invite`.
- **Ready before the first sign-in**: `dth init` asks the prerequisites (target, address, sign-in and SSO,
  owners, model, git host) and writes a settings file; the Hub applies settings files at every start
  (`DTH_SETTINGS_FILE`, `DTH_SETTINGS`, Helm `settings.*`, Terraform `hub_settings`), including sign-in,
  SSO and users. See [`docs/settings-file.md`](settings-file.md).
- **Users and sign-in**: add, edit, disable and remove users, password links for invites and resets, and
  single sign-on set up in the UI for Google Workspace, Microsoft Entra ID, Okta, Keycloak or any OIDC
  provider ([`docs/users-and-sign-in.md`](users-and-sign-in.md)).
- **Ask that looks further**: when one round of retrieval finds too little, the model searches again,
  reads files and lists paths before answering, within the reader's access; answers are cached until
  their sources change, and Claude calls use prompt caching.

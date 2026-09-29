# Plan: DocTheRepo Hub — One-Stop Engineering Intelligence

> **Status: APPROVED (v3) — all decisions confirmed; implementation follows § 14.**
> This plan supersedes `plan-docrag-orchestrator.md`. Everything that plan got right (AST triage, scoped
> context, spend guard, push modes, zero-downtime reindex, typed errors) is carried forward; everything the
> new vision contradicts (no UI, no Confluence, single-node only, bbolt/NATS state) is replaced here.

### Decisions this draft assumes (confirm or change each one)

| # | Decision | Assumed choice | Source |
|---|---|---|---|
| D1 | Users | Internal teams (not customer-facing), Okta + Google Workspace SSO via OIDC, roles Owner/Admin/Editor/Viewer, per-repo access | You confirmed |
| D2 | Backend language | Go 1.25 (current supported release; 1.23 reached end of support in 2025 and current pgx/migrate releases require 1.25) — reasons and rejected alternatives in § 3.1 | You delegated → chosen |
| D3 | Web UI | React 18 + TypeScript + Vite, embedded in the Go binary (§ 3.1) | You delegated → chosen |
| D4 | System of record | PostgreSQL 16 + pgvector (relational + graph + vectors + job queue in one store) | **Suggested — confirm** |
| D5 | Sensitive data | Customers bring their own LLM/keys, so PII policy is theirs: **credential/secret scrubbing always on** (a leaked AWS key in a prompt is never acceptable); **PII redaction is a per-provider toggle, default OFF** | You answered → adjusted (see § 8.8) |
| D6 | Known issues | Marked from the Inbox list, linked from Jira/Confluence, or pasted as text — the Hub explains each and derives a matching rule; plus auto-suggestions a human confirms | You confirmed |
| D7 | Doc storage | Tree (Markdown in repo) + Palace (knowledge graph) + Library (curated catalog) — why and where in § 4.4 | You confirmed |
| D8 | Milestone order | M1 docs/RAG/Q&A/UI/BYO-LLM/analytics → M2 errors+alerts+known issues → M3 Confluence/Jira/Wiz/Splunk → M4 hardening | You confirmed |
| D9 | Scale target | 30–40 repos (design to 100); **millions of log lines per 10 minutes** at the sources plus heavy event-bus traffic → the Hub never ingests raw logs; it ingests source-filtered errors and aggregates them at the edge (§ 4.5, § 8.9); ≤ 100 concurrent UI users | You answered |
| D10 | Deployment | Local (Docker Compose, one command) + AWS (ECS Fargate + RDS) + GCP (Cloud Run + Cloud SQL) + Helm (EKS/GKE) | You confirmed "local and cloud both" |
| D11 | Interfaces | Web UI + REST API + CLI, all on the same API | You confirmed |
| D12 | Connectors | GitHub, GitLab, AWS CloudWatch, GCP Logging/Monitoring, Datadog, Grafana, Prometheus Alertmanager, Sentry, PagerDuty, Opsgenie, Wiz, Splunk, Confluence, Jira, generic JSON webhook, event-bus inspectors (D14) | You confirmed |
| D13 | LLM | Bring your own: Anthropic, OpenAI, Azure OpenAI, AWS Bedrock, Google Vertex AI, any OpenAI-compatible endpoint, Ollama (local). The Hub ships no keys and no model access | You confirmed |
| D14 | Event platforms | **All of them**: Kafka (self-managed, Amazon MSK, Confluent), SQS, SNS, EventBridge, Kinesis Data Streams, GCP Pub/Sub, RabbitMQ — covering DLQs, consumer lag/backlog, and error logs from consuming services (§ 8.20) | You confirmed |

---

## 1. Project Overview

DocTheRepo Hub is a self-hosted engineering intelligence platform that connects an organization's code
repositories, cloud consoles (AWS, GCP), monitoring and alerting tools (Datadog, Grafana, Prometheus,
Sentry, PagerDuty, Opsgenie), security and log platforms (Wiz, Splunk), and knowledge base (Confluence)
into one searchable, continuously updated brain. On every repository push it structurally diffs the code,
regenerates only the affected documentation, and re-indexes it, so engineers and AI agents can ask
questions about any repo or the whole architecture and get cited, current answers. It pulls errors and
alerts from every connected tool into one inbox, groups duplicates, and "decodes" each new problem into a
plain-English explanation linked to the code, docs, recent commits, and owners involved — while a
known-issues registry suppresses errors the team already understands or cannot act on, so no LLM spend is
wasted on them. It runs on the operator's own infrastructure (a laptop via one command, or AWS/GCP via
Terraform/Helm) with the operator's own LLM keys, and an analytics page shows exactly what was spent,
where, and what was saved.

## 2. Goals & Non-Goals

### Goals

**Repos, docs, and search (M1)**
1. Connect any number of GitHub (GitHub App) and GitLab (SaaS or self-managed, access token) repositories;
   every `push` to a tracked branch triggers the pipeline via webhook, or via polling when the Hub has no
   public URL, with identical downstream processing.
2. Classify each push with Tree-sitter AST diffing as cosmetic (`ABORT`, zero LLM cost) or structural
   (`PROCEED`) for Go, Java, Python, TypeScript/JavaScript, and Rust, with an LLM fallback for other files.
3. For structural changes, regenerate only the affected documentation sections and land them in the repo
   under an agent-owned path (default `docs/generated/`) via direct commit, PR + auto-merge (default), or PR
   + human approver — never writing outside that path.
4. Keep three documentation views continuously in sync with code: the **Tree** (Markdown mirroring code
   structure), the **Palace** (knowledge graph of services, APIs, env vars, dependencies, errors, owners),
   and the **Library** (topic-organized catalog) — all updated by the same push pipeline within 5 minutes of
   the push at p95 (excluding LLM provider latency above 3 minutes).
5. Answer natural-language questions about one repo, a set of repos, or the whole estate through the web
   UI, API, and CLI, with every claim cited to a file/line, doc section, Confluence page, or issue, and with
   answers restricted to repositories the asking user can access.
6. Remove stale index content within one pipeline run when code or docs are deleted, renamed, or reverted.

**Errors, alerts, known issues (M2)**
7. Ingest errors and alerts from AWS CloudWatch (Logs + Alarms), GCP Cloud Logging / Error Reporting /
   Cloud Monitoring, Datadog, Grafana, Prometheus Alertmanager, Sentry, PagerDuty, and Opsgenie, normalized
   into one event schema — sustaining **20,000 error events/s** of burst ingest (3 api replicas) while the
   sources themselves produce millions of log lines per 10 minutes, by filtering at the source and
   aggregating by fingerprint at the edge (§ 8.9).
7a. Surface event-platform failures (dead-letter queues, consumer lag, poison messages) for the buses named
   in D14 as Issues alongside errors and alerts.
8. Group events into **Issues** by a deterministic fingerprint so 10,000 occurrences of the same error cost
   at most one LLM decode.
9. Decode each new Issue into: plain-English summary, probable cause, affected service/code (linked to repo
   files and docs), recent commits touching that code, similar past issues, and suggested next steps.
10. Maintain a **Known Issues registry** fed three ways — "Mark as known" on any Inbox row, a Jira issue or
    Confluence page link, or pasted free text — where the Hub explains the issue in plain English and
    proposes the matching rule for a human to confirm; any event matching an active rule is suppressed
    before any LLM call and counted as savings.

**Knowledge and security (M3)**
11. Sync Confluence spaces and Jira projects (incrementally, read-only) as context for doc generation,
    decoding, and Q&A, and import Confluence pages / Jira issues labelled as known issues or runbooks into
    the Known Issues registry.
12. Ingest Wiz security issues/vulnerabilities and Splunk saved-search results/alerts into the same inbox.

**Platform (all milestones)**
13. Bring your own LLM: operators register providers and keys, and route each feature (doc generation,
    Q&A, error decode, triage fallback, embeddings) to a chosen provider/model.
14. Enforce spend ceilings per day, per job, per feature, and per provider, with a hard stop; `dry-run`
    previews cost without spending.
15. Show an **Activity & Analytics** page: tokens/cost by provider, model, feature, repo, and user;
    LLM calls avoided (triage aborts, known-issue suppressions, cache hits); pipeline latency; connector
    health; audit trail.
16. Install locally with one command (`dth up`) and deploy to AWS or GCP with one Terraform apply, all from
    the same container image.

### Non-Goals
- **No write actions against cloud or monitoring tools.** The Hub reads; it never restarts services,
  acknowledges/resolves PagerDuty incidents, silences alerts at the source, or changes IAM. All cloud
  credentials are read-only.
- **Not a paging or alerting system.** It does not notify on-call; it enriches what the existing tools
  already alert on. (Outbound Slack/Teams digests are listed in § 15 as a candidate, not in scope.)
- **Not a log store or SIEM.** Normalized events are retained 30 days (configurable) for grouping and
  analytics; raw logs stay in CloudWatch/Splunk/etc.
- **No hosted multi-tenant SaaS.** One deployment serves one organization.
- **No git hosts beyond GitHub and GitLab** in this plan (Bitbucket/Azure DevOps fit the connector SDK later).
- **No editing of human-authored docs outside the generated path**, and no writing back to Confluence.
- **No model training or fine-tuning.**
- **No mobile app.** The web UI is responsive down to 360px for reading answers and the inbox.
- **No shell-output token-compression wrappers** (RTK etc.) — rejected on measured evidence in the
  original plan; scoped AST context is the token-reduction mechanism.

## 3. Technology Stack

| Layer | Technology | Why chosen |
|---|---|---|
| Backend language | Go 1.25 | Carried from the original plan (required by you): one static binary, low memory, strong concurrency for connector polling |
| HTTP router | `net/http` + `go-chi/chi/v5` | Standard-library-first; chi adds routing/middleware only |
| Web UI | React 18 + TypeScript 5 + Vite 5 | Largest ecosystem for data-heavy UIs; Vite gives fast builds; output is static files embedded into the Go binary via `embed` so there is still one artifact |
| UI components | Tailwind CSS 3 + shadcn/ui (Radix primitives) | Accessible primitives, no runtime theming library, easy dark mode |
| UI data layer | TanStack Query 5 + React Router 6 | Server-state caching and polling for the inbox/analytics without a global store |
| Charts | Recharts | Declarative charts for the analytics page |
| Graph view (Palace) | Cytoscape.js | Handles thousands of nodes with layout algorithms; used for the architecture/palace explorer |
| Markdown rendering | react-markdown + remark-gfm + Mermaid | Renders generated docs and architecture diagrams in the UI |
| Primary database | PostgreSQL 16 + pgvector 0.7 | One store for relations, graph edges, vectors (HNSW), full-text (tsvector), and the job queue (`SKIP LOCKED`). Managed everywhere (RDS, Cloud SQL, AlloyDB), so cloud deploy adds no new service type. Replaces the original bbolt + NATS + Qdrant trio |
| Vector store (alt adapter) | Qdrant | Kept as a pluggable adapter for teams with very large indexes; pgvector is the default |
| DB access | `jackc/pgx/v5` + `sqlc` | Type-safe generated queries, no ORM magic, first-class pgvector via `pgvector/pgvector-go` |
| Migrations | `golang-migrate/migrate` | Plain SQL up/down files, runs at startup behind an advisory lock |
| Job queue | Postgres table + `FOR UPDATE SKIP LOCKED` + advisory locks | Exactly-once claim, per-repo serialization, zero extra service; scales to the D9 target (≈ 2 jobs/s) with large headroom |
| AST parsing | `tree-sitter/go-tree-sitter` + 5 official grammars | Language-agnostic diffing and chunking |
| GitHub | `google/go-github` + GitHub App auth | Canonical client |
| GitLab | `gitlab.com/gitlab-org/api/client-go` | Official client, SaaS + self-managed |
| AWS | `aws-sdk-go-v2` (cloudwatchlogs, cloudwatch, sts) | Official SDK; STS AssumeRole for cross-account read-only access |
| GCP | `cloud.google.com/go/logging`, `errorreporting/apiv1beta1`, `monitoring/apiv3` | Official clients; Workload Identity Federation / service account auth |
| Other connectors | Plain `net/http` against documented REST/GraphQL APIs (Datadog, Sentry, PagerDuty, Opsgenie, Wiz GraphQL, Splunk REST, Confluence REST v2) | Their Go SDKs are either unofficial or heavy; the Hub needs a narrow read-only subset |
| HTML→Markdown (Confluence) | `golang.org/x/net/html` + in-house converter | Confluence storage format is XHTML; a narrow converter keeps headings/tables/code blocks which is all chunking needs |
| LLM providers | Plain HTTP adapters per provider API; Bedrock via `aws-sdk-go-v2/bedrockruntime`, Vertex via REST + ADC | Keeps the provider layer uniform and avoids 6 SDKs with different retry/streaming semantics |
| Auth (UI) | OIDC via `coreos/go-oidc/v3` + `golang.org/x/oauth2`; server-side sessions in Postgres | Works with Okta, Azure AD/Entra, Google Workspace, Keycloak; sessions are revocable |
| Auth (API/CLI) | Personal access tokens (random 32 bytes, stored as SHA-256) | Simple, revocable, scoped |
| Secret encryption | AES-256-GCM envelope encryption; KEK from AWS KMS / GCP KMS / local key file | Connector credentials and LLM keys are stored encrypted; cloud KMS in cloud, file key locally |
| CLI | Cobra | De facto Go CLI framework |
| Config | YAML (`gopkg.in/yaml.v3`) + env vars; runtime settings in DB (editable from UI) | Bootstrap config in a file; connectors/providers/rules managed live from the UI |
| Logging | `log/slog` JSON | Stdlib structured logging |
| Metrics | Prometheus text exposition (`prometheus/client_golang`) | Standard scrape format |
| Tracing | OpenTelemetry Go SDK, OTLP exporter (off by default) | Vendor-neutral; ships to Datadog/Grafana Tempo/Cloud Trace |
| Backend tests | `testing` + `testify` + `testcontainers-go` (Postgres+pgvector, Qdrant, LocalStack) | Real databases, no in-memory fakes |
| Frontend tests | Vitest + React Testing Library; Playwright for E2E | Standard for React/Vite |
| Performance tests | k6 | Scriptable HTTP load, burst scenarios |
| Container | Docker, distroless base image | Single image for Compose, ECS, Cloud Run, Helm |
| Local deploy | Docker Compose (Hub + Postgres/pgvector + optional Ollama) | `dth up` drives it |
| AWS deploy | Terraform: ECS Fargate, RDS PostgreSQL 16, ALB + ACM, Secrets Manager, KMS, CloudWatch | Serverless containers, managed Postgres with pgvector support |
| GCP deploy | Terraform: Cloud Run, Cloud SQL PostgreSQL 16, Secret Manager, Cloud KMS, Serverless VPC connector | Same shape as AWS on GCP primitives |
| Kubernetes | Helm chart (EKS, GKE, any K8s) | For teams already on Kubernetes |
| Security scanning | `govulncheck`, `npm audit --omit=dev`, Trivy on the image | CI gates |
| High-volume stream ingest (AWS) | CloudWatch Logs subscription filter → Amazon Data Firehose → Hub HTTPS endpoint | Push-based, filtered at source, batched and retried by AWS; avoids polling millions of lines through rate-limited read APIs |
| High-volume stream ingest (GCP) | Log Router sink (filtered) → Pub/Sub → Hub pull subscriber (`cloud.google.com/go/pubsub`) | Same pattern on GCP; Pub/Sub gives backpressure and replay |
| Jira | Plain `net/http` against Jira Cloud REST v3 / Data Center REST v2 (read-only) | Known issues and context from tickets; narrow read-only subset |
| Kafka / MSK / Confluent | `twmb/franz-go` (+ `kadm`, `pkg/sasl/aws` for MSK IAM) | Pure Go (no librdkafka/cgo), admin API for consumer-group lag, SASL/SCRAM, mTLS and MSK IAM auth |
| SQS, SNS, EventBridge, Kinesis | `aws-sdk-go-v2` service clients + CloudWatch metrics | DLQ depth/age, delivery failures, iterator age — official SDK |
| Pub/Sub backlog & dead letters | `cloud.google.com/go/pubsub` + Cloud Monitoring metrics | Backlog (`num_undelivered_messages`, `oldest_unacked_message_age`) and dead-letter subscriptions |
| RabbitMQ | Management HTTP API via `net/http` | Queue depth, DLX queues, consumer counts without an AMQP client |

### 3.1 Why Go + React, and why not the alternatives

**Backend — ranked for this project** (30–40 repos, millions of log lines at the sources, 15+ connectors,
local one-command install and cloud deploy, must be easy to extend):

| # | Option | Fit | Why it fits THIS project | Watch out for |
|---|---|---|---|---|
| 1 | **Go 1.25** | ★★★★★ | Goroutines make 15+ concurrent pollers/stream consumers and 20k events/s aggregation simple and cheap; one static binary (~40 MB image) for `dth up`, ECS, Cloud Run, Helm; ~50 MB RAM per replica and < 1s startup (Cloud Run friendly); official Tree-sitter bindings; official AWS, GCP, GitHub, GitLab SDKs; the infra ecosystem the Hub sits next to (Kubernetes, Terraform, Prometheus, Grafana) is Go | More boilerplate than Java/Kotlin; no heavyweight AI framework (not needed — LLM calls are plain HTTP) |
| 2 | Java 17 + Spring Boot 3 | ★★★★☆ | Excellent Spring Security OIDC (Okta/Google), mature enterprise integrations, your team's interview language | Official Tree-sitter Java bindings (`jtreesitter`) need Java 22+ (Foreign Function & Memory API) — conflicts with Java 17; 300–500 MB RAM per replica and slow cold starts raise Fargate/Cloud Run cost |
| 3 | Kotlin + Ktor/Spring | ★★★☆☆ | Concise, coroutines for concurrency | Same JVM Tree-sitter and memory issues; smaller hiring pool |
| 4 | Python + FastAPI | ★★★☆☆ | Richest AI/RAG libraries | GIL limits CPU-bound fingerprinting/AST work at 20k events/s; packaging a one-command local install is heavier; dynamic typing hurts a 15-connector codebase |
| 5 | TypeScript + Node | ★★☆☆☆ | Same language as the UI | Single-threaded CPU work (AST parsing, regex redaction) blocks the event loop under log storms; native Tree-sitter builds are fragile |

→ **Pick: Go** — the workload is I/O-heavy concurrency plus CPU-bound parsing, shipped as a self-hosted
binary; Go is the best fit for all three at once.

**How it stays easy to extend**: every integration is a Go interface in `internal/ports` with a
registration call, so a new connector is one package plus a fixture test; teams that do not write Go can
add doc-generation engines and context sources in any language through the versioned JSON/HTTP contracts
(`llm/externalcli`, `contextprovider/command|http`), and add log sources by pointing any tool at the
generic signal webhook (`/hooks/generic/{id}`, § 7.1) with a JSON field mapping configured in the UI.

**Frontend — ranked**:

| # | Option | Fit | Why it fits THIS project | Watch out for |
|---|---|---|---|---|
| 1 | **React 18 + TypeScript + Vite** | ★★★★★ | Largest ecosystem for exactly these screens: streaming chat, graph explorer (Cytoscape), charts (Recharts), Markdown/Mermaid; easiest hiring | Needs discipline on state (solved by TanStack Query) |
| 2 | Vue 3 + TypeScript | ★★★★☆ | Simpler learning curve | Smaller ecosystem for graph and chart components |
| 3 | Angular 17 | ★★★☆☆ | Batteries included, strong for large enterprise forms | Heavier, slower to iterate on a chat/graph-centric UI |
| 4 | Server-rendered Go templates + htmx | ★★☆☆☆ | No JS build step | Streaming chat and an interactive graph explorer become awkward |

→ **Pick: React + TypeScript** — the product's core screens (Ask, Palace graph, Analytics) are
component-library-heavy, which is React's strongest ground.

## 4. Architecture

**Pattern**: **Modular monolith with Hexagonal (Ports & Adapters) modules and an event-driven internal
pipeline.** One Go binary, three roles selectable by flag — `api` (HTTP + UI), `worker` (queue consumers),
`scheduler` (connector polling, GC, PR lifecycle, rollups). Locally all three run in one process; in the
cloud they run as separate services from the same image and scale independently.

**Why this pattern**: Every external system (13 connectors, 7 LLM providers, 2 vector stores, 2 git hosts)
must be swappable and testable in isolation — that is the hexagonal requirement. The work is inherently
asynchronous (webhooks must return in < 1s; doc generation takes minutes; event bursts arrive in hundreds per
second) — that is the event-driven requirement. Microservices were rejected: at D9 scale a single team
operates this, and splitting into services would add network hops, distributed transactions, and N
deployment pipelines for no throughput need. Postgres is the single system of record and the queue, so the
roles can be scaled horizontally without distributed coordination beyond row locks and advisory locks.

### 4.1 Overall architecture (wide view)

```
                               ┌───────────────────────── USERS ──────────────────────────┐
                               │  Web UI (React)      REST API clients      dth CLI        │
                               └───────────┬───────────────────┬───────────────┬──────────┘
                                           │ HTTPS (OIDC session / PAT)                    
┌──────────────────────────── DocTheRepo Hub (one Go binary, 3 roles) ───────────────────────────────┐
│                                                                                                     │
│  ROLE: api                                                                                          │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────┐ │
│  │ Auth / RBAC  │ │ Q&A (SSE)    │ │ Inbox API    │ │ Docs/Library │ │ Analytics    │ │ Admin:   │ │
│  │ OIDC, PAT    │ │ RAG engine   │ │ issues,known │ │ tree, palace │ │ usage, audit │ │connectors│ │
│  └──────────────┘ └──────┬───────┘ └──────────────┘ └──────────────┘ └──────────────┘ │providers │ │
│  ┌──────────────────────────────────────────────┐                                     └──────────┘ │
│  │ Ingress: /hooks/{connector}/{id}  (verify → normalize → enqueue → 202)                          │ │
│  └───────────────────────┬──────────────────────┘                                                 │
│                          │ enqueue (jobs table)                                                    │
│  ROLE: worker            ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────────────────────────┐ │
│  │                                   DOMAIN CORE (pure logic)                                    │ │
│  │  Code pipeline:  triage(AST) → chunk → manifest diff → scoped context → spend guard →         │ │
│  │                  docgen → land docs → embed/index → palace update → library refresh           │ │
│  │  Signal pipeline: normalize → redact → fingerprint → group → known-issue match →              │ │
│  │                   (suppress | reuse decode | spend guard → decode with code/doc context)      │ │
│  │  Knowledge pipeline: sync page → convert → chunk → embed → link to palace entities            │ │
│  │  Q&A: retrieve (vector + keyword + graph) → ACL filter → rerank → answer w/ citations         │ │
│  └──────────────────────────────────────────────────────────────────────────────────────────────┘ │
│        │ ports only                                                                                │
│  ROLE: scheduler: connector polls · PR lifecycle sweep · GC · usage rollups · staleness checks     │
└────────┼───────────────────────────────────────────────────────────────────────────────────────────┘
         │
 ┌───────┴──────────────────────────── ADAPTERS (ports & adapters) ─────────────────────────────────┐
 │ CodeHost: GitHub, GitLab        Signal sources: CloudWatch, GCP Logging/ErrorReporting/Monitoring, │
 │ LLM: Anthropic, OpenAI, Azure,   Datadog, Grafana, Alertmanager, Sentry, PagerDuty, Opsgenie,      │
 │      Bedrock, Vertex, OpenAI-   Wiz, Splunk                                                        │
 │      compatible, Ollama         Knowledge: Confluence, Jira                                        │
 │ Embeddings: same providers      Vector: pgvector (default), Qdrant                                 │
 │ Push: direct, pr_auto_merge,    Secrets: KMS (AWS/GCP), local key file                             │
 │       pr_with_approver          Queue/Store: PostgreSQL                                            │
 └────────────────────────────────────────────────────────────────────────────────────────────────┘
         │
 ┌───────┴──────────────── PostgreSQL 16 + pgvector (system of record) ──────────────────────────────┐
 │ users/sessions/rbac · connectors (encrypted creds) · jobs queue · chunks+vectors+tsvector ·        │
 │ doc tree · palace entities/edges · library shelves · events (partitioned) · issues · known issues  │
 │ · decodes · qa threads · answer cache · usage events + rollups · spend ledger · PRs · audit log    │
 └────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 4.2 Deployment topologies

```
LOCAL (dth up)                         AWS (terraform/aws)                      GCP (terraform/gcp)
┌──────────────────────┐   ┌─────────────────────────────────────┐  ┌──────────────────────────────────┐
│ docker compose       │   │ Route53 → ALB (ACM TLS)             │  │ HTTPS LB (managed cert)          │
│  hub (all 3 roles)   │   │   → ECS Fargate svc: api  (2..N)    │  │   → Cloud Run: api (min 1)       │
│  postgres+pgvector   │   │   → ECS Fargate svc: worker (1..N)  │  │   → Cloud Run: worker (min 1,    │
│  ollama (optional)   │   │   → ECS Fargate svc: scheduler (1)  │  │       CPU always allocated)      │
│ polling ingest if no │   │ RDS PostgreSQL 16 (pgvector)        │  │   → Cloud Run: scheduler (1)     │
│ public URL           │   │ Secrets Manager + KMS               │  │ Cloud SQL PostgreSQL 16          │
│ http://localhost:8080│   │ IAM task role → AssumeRole into     │  │ Secret Manager + Cloud KMS       │
└──────────────────────┘   │   read-only roles in watched accts  │  │ Workload Identity for GCP reads  │
                           └─────────────────────────────────────┘  └──────────────────────────────────┘
KUBERNETES (helm): Deployments api / worker / scheduler(replicas: 1) + external Postgres (RDS/Cloud SQL/in-cluster)
```

### 4.3 Primary data flows

**Flow A — push → docs → index (auto-update)**
1. GitHub/GitLab sends `push` to `POST /hooks/github/{connector_id}` (or the scheduler's poller detects a
   new head SHA).
2. Ingress verifies the signature, applies the bot-loop guard (drops pushes authored by the Hub's own bot or
   touching only the generated-docs path), inserts a `code_push` job, returns `202` in < 500 ms.
3. A worker claims the job (`SKIP LOCKED`) holding the repo's advisory lock, so pushes to one repo run in
   order and different repos run in parallel.
4. Fetch diff + blobs → Tree-sitter AST diff → **triage**. Cosmetic → `aborted`, zero spend.
5. Chunk changed files, diff against the chunk manifest (added/changed/removed), detect pure renames
   (re-key without re-embedding).
6. Assemble scoped context (changed symbol bodies + one-hop caller/callee signatures + referenced types +
   linked Confluence sections + known issues for this service), capped by token budget.
7. **Spend guard** checks per-job, per-day, per-feature, per-provider ceilings; blocks before any paid call.
8. DocGen (routed provider) returns Markdown sections → validated against the contract → one repair-retry.
9. Push adapter lands docs (PR + auto-merge by default).
10. Embed changed code + doc chunks → upsert; soft-delete removed chunks.
11. Update the **Palace** (entities/edges extracted from the AST: services, endpoints, env vars, deps) and
    refresh affected **Library** shelves.
12. Write actual usage to `usage_events`; job `done`. Every step logs with the job's correlation ID.

**Flow B — error/alert → decoded issue**
1. Webhook (Sentry, PagerDuty, Datadog, Grafana, Alertmanager, Opsgenie, Splunk, Wiz, AWS via EventBridge
   API destination, GCP via Monitoring webhook channel) or poller (CloudWatch Logs, GCP Logging, Splunk
   saved searches, Wiz issues) delivers raw payloads.
   High-volume log sources arrive pre-filtered via Firehose (AWS) or Pub/Sub (GCP) — § 4.5.
2. Adapter maps each to a normalized `SignalEvent` (idempotent on `source + external_id`).
3. Scrub secrets (always) and PII (if enabled for the routed provider) → fingerprint → in-memory 1s
   aggregation → one upsert per fingerprint into `issues` (`occurrences += n`, last_seen) + reservoir
   sample into `event_samples` + minute counters.
4. Match against **Known Issues** rules → matched: mark `suppressed`, record avoided cost, stop.
5. Group already decoded and its fingerprint unchanged → reuse decode, stop.
6. New group → enqueue `decode_issue` job → map stack frames/service to repo code via the Palace and
   symbol search → assemble context (code, docs, commits in the last 7 days touching those files, similar
   decoded issues, Confluence runbooks) → spend guard → LLM decode → store → visible in Inbox.

**Flow C — question → cited answer**
1. User asks in the UI (SSE stream), API, or `dth ask`.
2. Answer-cache lookup keyed by normalized question + scope + index version → hit returns immediately.
3. Hybrid retrieval: pgvector ANN (top 40) + Postgres full-text (top 40) fused by Reciprocal Rank Fusion
   → 1-hop Palace expansion from top entities → ACL filter by the user's repo access → top 12 chunks.
4. Spend guard → LLM answer constrained to cite chunk IDs → citations resolved to links → streamed.

### 4.4 Why Tree + Palace + Library (and where each is used)

One source of truth, three shapes, because the three audiences ask three different kinds of question:

| Shape | What it is | Answers questions like | Where it is used | Why it beats the alternative |
|---|---|---|---|---|
| **Tree** | Generated Markdown mirroring the code (`repo → dir → file → symbol`), committed in each repo under `docs/generated/`, mirrored in `doc_nodes` | "What does `OrderService.refund` do?", "What's in this module?" | Docs page in the UI; PR reviews (docs change in the same review flow as code); AI coding agents reading the repo directly | Updates are **surgical** — a push regenerates only the changed symbols' sections, so cost scales with the change, not the repo. A wiki-style doc drifts; a tree keyed to code cannot drift without the pipeline noticing |
| **Palace** | Knowledge graph of entities (service, endpoint, env var, datastore, topic, dependency, cloud resource, issue, known issue, page, owner) and links (calls, exposes, reads_env, publishes, subscribes, raises, runbook_for, owned_by) | "What breaks if the `payments` topic schema changes?", "Which service owns this CloudWatch error, what code raised it, who owns it, is there a runbook?", "How does auth work across the platform?" | Error decoding (error → service → code → recent commits → owner → runbook → known issue); Q&A graph expansion; Palace explorer page; impact analysis | Vector search alone finds *similar text*; it cannot follow "publishes → subscribes" across 40 repos. The graph is built deterministically from code, so it is cheap and never hallucinated. For your event-heavy estate, the publish/subscribe edges are what connect an event error to both the producer and the consumer |
| **Library** | Topic catalog on top of both: Architecture, Services, APIs, Events & Topics, Data, Config & Env, Dependencies, Runbooks, Known Issues, Security, Decisions, Confluence, Jira | "Show me every runbook", "List all public APIs", "What are our known issues for checkout?" | Library page for browsing and onboarding; scoping Q&A ("ask only within Runbooks"); curated shelves pinned by team leads | Humans browse by topic, not by file path. Shelves are rule-driven (no LLM), so they stay current automatically, and curators can pin/annotate without the pipeline overwriting them |

**Benefits for you specifically**
- **Lower LLM spend**: the Tree makes doc updates incremental; the Palace resolves most "which code/owner/
  runbook" lookups with SQL instead of LLM calls; decode prompts get the 5 relevant chunks instead of whole
  repos.
- **Faster incident triage**: an alert in the Inbox already shows the producing and consuming services, the
  code, the last 7 days of commits to it, the owner, and any runbook or known issue — one screen.
- **Onboarding**: a new engineer reads the Library's Architecture and Services shelves, then drills into
  the Tree.
- **Extending docs by hand**: engineers add context inside `<!-- dth:human -->` blocks in the Tree, pin
  items and write notes on Library shelves, and link Confluence/Jira pages to Palace entities — none of it
  is overwritten by regeneration.

### 4.5 High-volume signal architecture (millions of log lines per 10 minutes)

```
 SOURCES (your accounts)                    HUB (api replicas × N)                         POSTGRES
 ┌──────────────────────────┐   filtered   ┌──────────────────────────────────────────┐
 │ CloudWatch Logs          │─subscription─▶ Firehose HTTP endpoint ┐                   │
 │  (ERROR/Exception only)  │  → Firehose  │                        │                   │
 │ GCP Log Router sink      │─▶ Pub/Sub ──▶│ Pub/Sub pull consumer  ├─▶ normalize        │
 │  (severity>=ERROR)       │              │                        │   → scrub secrets │
 │ Sentry / Datadog / PD /  │─webhooks────▶│ Webhook ingress        │   → fingerprint   │   issues
 │  Opsgenie / Grafana / AM │              │                        │   → known-issue ──┼─▶ (counters,
 │ Splunk saved searches    │─poll/alert──▶│ Pollers (scheduler)    │     match          │    batched 1s)
 │ Wiz issues               │─poll────────▶│                        │   → in-memory      │   event_samples
 │ Event buses (D14): DLQs, │─poll────────▶│ Bus inspectors         ┘     aggregation ───┼─▶ (reservoir,
 │  consumer lag            │              │                              window 1s)     │    ≤ 20/fp/hour)
 └──────────────────────────┘              └──────────────────────────────────────────┘   decode jobs
                                                                                          (only NEW fps)
```
Three rules keep this cheap at your volume:
1. **Filter at the source.** Only error-level lines leave CloudWatch/GCP (subscription filter / sink
   filter); info/debug logs never reach the Hub. Filters are created by Terraform in `readonly-roles` and are
   editable per log group.
2. **Aggregate at the edge.** Each replica groups events by fingerprint in memory for 1 second and flushes
   one upsert per fingerprint (`occurrences += n`), so Postgres writes scale with *distinct errors*, not
   with log volume. An incident producing 50,000 identical errors per second is one row update per second.
3. **Sample, don't store.** Per fingerprint, keep the first 5 events plus a reservoir sample of 20 per hour;
   minute-level counts go to `issue_counts_minutely`. Raw logs stay in CloudWatch/Splunk (non-goal: not a
   log store).

### 4.6 Boundaries
- **Domain core** (`internal/core/...`) owns triage, chunking, manifest diff, scoped context, spend guard,
  fingerprinting, redaction, known-issue matching, retrieval fusion, and prompt assembly. It performs no
  I/O and imports no adapter.
- **Adapters** each own exactly one external system and translate to/from domain types.
- **Store** (`internal/store`) owns SQL; the core sees repository interfaces only.
- **API layer** owns HTTP, auth, RBAC enforcement, and DTO mapping — no business logic.
- **Web UI** is a pure client of the public REST API; it has no private endpoints.

### 4.7 Scale estimates (from D9)
| Quantity | Estimate | Basis |
|---|---|---|
| Pushes | 40 repos × ~30/day = 1,200/day; ~20% PROCEED → ~240 doc-gen jobs/day | Typical active repo; triage aborts the rest at zero LLM cost |
| Index size | 40 repos × ~5k chunks = 200k chunks × 1,536 dims × 4 B ≈ 1.2 GB vectors, ~2.5 GB with HNSW | pgvector; fits a db.r6g.large / Cloud SQL 2 vCPU 8 GB |
| Source logs | "Millions per 10 min" → assume 5M/10 min ≈ 8,300 lines/s | Your answer |
| Error-level after source filter | Assume ≤ 5% steady ≈ 400/s; incident bursts to 20,000/s | Design target; to be measured in Phase 10 |
| Postgres signal writes | ≤ distinct fingerprints/s (≈ hundreds) | 1s edge aggregation |
| Decode LLM calls | ≈ new fingerprints/day (hundreds), not events | Grouping + known-issue suppression + decode reuse |
| Q&A | 100 users × 20 questions/day = 2,000/day | Answer cache absorbs repeats |

### 4.8 Key trade-offs
| Decision made | Alternative | Why this choice |
|---|---|---|
| Modular monolith, 3 roles | Microservices | One team operates it; roles still scale independently from one image |
| PostgreSQL + pgvector for everything | Postgres + Qdrant + Kafka + Neo4j | One managed service in AWS/GCP; at this scale graph (recursive CTE), vectors (HNSW), queue (SKIP LOCKED), full-text all fit; Qdrant kept as an adapter if the index outgrows it |
| Filter at source + edge aggregation | Ship all logs into the Hub (e.g. via Kafka) | The Hub would become a second log platform; millions of lines/10 min would cost more than the LLM savings it exists to create |
| Postgres job queue | Kafka / SQS / NATS | Jobs are low-rate (hundreds/day to a few/s); exactly-once claim and per-repo ordering come free with row locks; no extra service |
| Deterministic palace extraction | LLM-extracted knowledge graph | Zero cost per push, reproducible, no hallucinated edges |
| Hybrid retrieval (vector + keyword + graph) | Vector-only RAG | Exact identifiers (error codes, function names) match keywords better than embeddings; graph adds cross-repo links |
| Known-issue rules require human confirmation | Auto-suppress by model | A wrongly suppressed real outage is the costliest failure mode |
| Consistency: strong (single Postgres primary) | Eventual, multi-region | Single-region internal tool; CAP choice is CP — during a DB outage ingress returns 503 and sources retry, rather than accepting data it cannot store |
| At-least-once delivery + idempotent handlers | Exactly-once transport | Sources (GitHub, Firehose, Pub/Sub, PagerDuty) are at-least-once anyway; idempotency keys make duplicates harmless |

## 5. Module / Component Breakdown

### Platform
- **config** — Loads bootstrap YAML + env, validates, exposes typed structs. Inputs: file/env. Outputs:
  `Config`. Deps: none.
- **store** — Postgres pool, migrations, sqlc queries, repository implementations. Outputs: repository
  interfaces (`JobRepo`, `ChunkRepo`, `GraphRepo`, `EventRepo`, `IssueRepo`, `UsageRepo`, ...).
- **secrets** — Envelope encryption of connector creds and LLM keys. Key interfaces: `KeyEncrypter`
  (adapters: `awskms`, `gcpkms`, `localfile`).
- **queue** — Job claim/ack/retry/dead-letter on Postgres, per-repo advisory-lock serialization, worker
  pool with per-job-type concurrency caps. Key interfaces: `Queue`, `Handler`.
- **scheduler** — Cron-like loop (leader via Postgres advisory lock) running connector polls, PR lifecycle
  sweep, chunk GC, event retention, usage rollups, known-issue expiry.
- **auth** — OIDC login, sessions, PATs, RBAC middleware, repo ACL resolution. Key types: `Principal`,
  `Role`, `RepoScope`.
- **observability** — slog, Prometheus metrics, OTel tracing, audit log writer.
- **api** — chi routes for every endpoint in § 7, SSE for Q&A, webhook ingress, embedded UI assets.

### Domain core (`internal/core`, pure)
- **triage** — AST-diff classification (`ABORT`/`PROCEED`) with semver dependency rule and LLM fallback
  request builder.
- **grammars** — Built-in + runtime-loaded Tree-sitter grammar registry, node-type maps per language.
- **chunker** — Code chunks per AST node; doc chunks per H2/H3 (split > 900 tokens); stable chunk IDs;
  rename detection.
- **manifest** — Delta computation (added/changed/removed) and generated-supersedes-imported precedence.
- **scopedcontext** — Minimal context assembly and budget-overflow drop order.
- **spendguard** — Estimates, multi-dimension ceilings, ledger decisions.
- **palace** — Entity/edge extraction from ASTs, configs, and signals; graph diffing.
- **library** — Shelf assignment rules (topic classification of doc sections) and catalog rendering.
- **signals** — Normalization helpers, secret scrubbing, PII redaction, fingerprinting, grouping.
- **aggregate** — Per-replica 1-second fingerprint aggregation, reservoir sampling, batched flush.
- **knownissues** — Rule matching engine (fingerprint, regex, attribute, time-boxed mute).
- **decode** — Context assembly and prompt/response contract for issue decoding.
- **rag** — Query normalization, hybrid retrieval fusion (RRF), graph expansion, ACL filtering,
  citation contract.
- **pipeline** — Orchestrates job types `code_push`, `decode_issue`, `knowledge_sync`, `import_docs`,
  `reindex`, `signal_batch` by calling ports.

### Ports (`internal/ports`) — interfaces only
`CodeHost`, `Push`, `DocGen` (via `LLM`), `LLM`, `Embedder`, `VectorIndex`, `SignalSource`,
`KnowledgeSource`, `ContextProvider`, `KeyEncrypter`, `Clock`. (Store repositories are defined in
`internal/store/repo.go` because they are persistence, not integrations.)

### Adapters (`internal/adapters/...`)
- **codehost/github**, **codehost/gitlab** — repos, diffs, blobs, trees, commits, branches, PR/MR ops,
  webhook verify/parse, rename info, webhook registration.
- **push/direct**, **push/prautomerge**, **push/prapprover** — landing strategies + PR lifecycle.
- **llm/anthropic**, **llm/openai**, **llm/azureopenai**, **llm/bedrock**, **llm/vertex**,
  **llm/openaicompat** (also covers Ollama chat) — chat/completion with usage reporting and streaming.
- **embed/openai**, **embed/bedrock**, **embed/vertex**, **embed/ollama**, **embed/openaicompat**.
- **vector/pgvector** (default), **vector/qdrant**.
- **signal/cloudwatch**, **signal/gcp**, **signal/datadog**, **signal/grafana**, **signal/alertmanager**,
  **signal/sentry**, **signal/pagerduty**, **signal/opsgenie**, **signal/wiz**, **signal/splunk**.
- **knowledge/confluence**, **knowledge/jira**.
- **signal/firehose** (AWS Data Firehose HTTP endpoint receiver), **signal/pubsub** (GCP Pub/Sub pull
  consumer), **signal/generic** (any tool posting JSON, field mapping configured in the UI),
  **signal/eventbus/kafka**, **/sqs**, **/sns**, **/eventbridge**, **/kinesis**, **/pubsub**,
  **/rabbitmq** — DLQ inspection, consumer lag/backlog, delivery-failure metrics (§ 8.20).
- **contextprovider/command**, **contextprovider/http** — extra context from operator tools.
- **secrets/awskms**, **secrets/gcpkms**, **secrets/localfile**.

### Interfaces
- **web** (`web/`) — React app: Ask, Inbox, Issue detail, Known Issues, Docs (Tree), Palace (graph),
  Library, Repos, Connectors, LLM Providers & Routing, Spend Limits, Activity & Analytics, Users & Access,
  Settings.
- **cmd/dth** — CLI: `up`, `down`, `status`, `ask`, `issues`, `known`, `connectors`, `repos`, `import`,
  `reindex`, `dry-run`, `retry`, `usage`, `token`, `adapter-test`, `migrate`.
- **cmd/hub** — server entrypoint (`--role=api,worker,scheduler`, default all).

## 6. Data Design

All tables live in the `public` schema of a dedicated `dth` database. `id` columns are UUIDv7 (time-ordered, index-friendly) unless stated.
Timestamps are `timestamptz`. JSON is `jsonb`.

### 6.1 Identity & access
| Table | Key fields | Indexes / notes |
|---|---|---|
| `users` | id, email (unique, citext), name, oidc_subject (unique), role enum(`owner`,`admin`,`editor`,`viewer`), disabled bool, created_at, last_login_at | First OIDC login creates `viewer`; first user ever becomes `owner` |
| `sessions` | id (random 32B, stored hashed), user_id FK, expires_at, created_at, ip, user_agent | idx(user_id); expired rows GC'd hourly |
| `api_tokens` | id, user_id FK, name, token_hash (sha256, unique), scopes text[], expires_at, last_used_at | Token shown once at creation |
| `repo_access` | user_id FK, repo_id FK, level enum(`read`,`admin`) | PK(user_id, repo_id). Default policy (config): `all_users_read_all_repos: true` |
| `groups`, `group_members`, `group_repo_access` | OIDC group claim → group → repos | Lets IdP groups grant repo access |
| `audit_log` | id, at, actor_user_id, action, target_type, target_id, details jsonb, ip | idx(at desc), idx(actor_user_id, at) |

### 6.2 Connectors, repos, LLM providers
| Table | Key fields | Notes |
|---|---|---|
| `connectors` | id, type enum(github, gitlab, cloudwatch, gcp, datadog, grafana, alertmanager, sentry, pagerduty, opsgenie, wiz, splunk, confluence), name, config jsonb (non-secret), creds_ciphertext bytea, creds_key_id, webhook_secret_hash, mode enum(`webhook`,`poll`,`both`), poll_interval, enabled, health enum(`ok`,`degraded`,`failing`,`unknown`), last_error, last_sync_at | Credentials never returned by the API |
| `connector_cursors` | connector_id, stream (e.g. log group), cursor text, updated_at | PK(connector_id, stream). Incremental sync position |
| `repos` | id, connector_id FK, full_name, default_branch, tracked_branch, docs_path, push_mode, last_processed_sha, service_name, owners text[], enabled | unique(connector_id, full_name) |
| `service_map` | service_name, repo_id, path_prefix, source_patterns jsonb | Maps signal `service`/log group/k8s namespace to repo code; auto-seeded, editable in UI |
| `llm_providers` | id, kind enum(anthropic, openai, azure_openai, bedrock, vertex, openai_compat, ollama), name, base_url, key_ciphertext, extra jsonb (region, deployment, project), enabled | |
| `model_routes` | feature enum(`docgen`,`qa`,`decode`,`triage`,`embedding`,`suggest`), provider_id FK, model, max_output_tokens, temperature, fallback_provider_id, fallback_model | PK(feature). One route per feature |
| `cost_table` | provider_kind, model, input_per_mtok_usd, output_per_mtok_usd, embed_per_mtok_usd, updated_by, updated_at | Operator-maintained; seeded with published list prices at build time, flagged as "verify" in UI |
| `embedding_lock` | index_name, provider_kind, model, dimensions, created_at | Immutability check; changing requires reindex |

### 6.3 Docs: Tree, Palace, Library, chunks
| Table | Key fields | Notes |
|---|---|---|
| `chunks` | chunk_id (text, `sha256(scope+"::"+path+"::"+symbol)[:16]`), repo_id nullable, source enum(`code`,`generated_doc`,`imported_doc`,`confluence`,`issue_decode`), path, symbol, language, content, content_hash, signature, commit_sha, embedding vector(D), tsv tsvector (generated), created_at, updated_at, deleted_at | PK(chunk_id). HNSW index on embedding (`vector_cosine_ops`, m=16, ef_construction=64); GIN on tsv; idx(repo_id, path); partial idx where deleted_at is null |
| `doc_nodes` (Tree) | id, repo_id, parent_id, kind enum(`repo`,`dir`,`file`,`section`), path, title, chunk_id nullable, order_key, updated_at | Mirrors `docs/generated/**` so the UI renders without git round trips |
| `entities` (Palace) | id, kind enum(`service`,`repo`,`module`,`file`,`symbol`,`endpoint`,`env_var`,`dependency`,`datastore`,`queue_topic`,`cloud_resource`,`issue`,`known_issue`,`confluence_page`,`team`,`person`), key (unique per kind, e.g. `endpoint:POST /orders`), name, repo_id, attrs jsonb, first_seen, last_seen, deleted_at | unique(kind, key); GIN on attrs |
| `edges` (Palace) | src_id, dst_id, kind enum(`contains`,`calls`,`exposes`,`reads_env`,`depends_on`,`uses_datastore`,`publishes`,`subscribes`,`documented_in`,`raises`,`owned_by`,`runbook_for`,`deployed_as`), weight, evidence jsonb (file/line/commit), last_seen, deleted_at | PK(src_id, kind, dst_id); idx(dst_id, kind) for reverse traversal |
| `library_shelves` (Library) | id, slug, title, description, rule jsonb (entity kinds, tags, path globs), curated bool, order_key | Seeded: Architecture, Services, APIs, Data & Storage, Config & Env, Dependencies, Runbooks, Known Issues, Security, Decisions (ADRs), Confluence |
| `shelf_items` | shelf_id, item_type enum(`doc_node`,`entity`,`confluence_page`,`known_issue`), item_id, pinned bool, note | PK(shelf_id, item_type, item_id) |
| `manifest_renames` | repo_id, old_path, new_path, commit_sha, at | Audit of rename re-keys |
| `prs` | repo_id, number, url, branch, base, job_id, mode, state, chunk_ids text[], approver, opened_at, updated_at | PK(repo_id, number); idx(state) for lifecycle sweep |

### 6.4 Signals, issues, known issues
| Table | Key fields | Notes |
|---|---|---|
| `event_samples` | id, connector_id, source_type, external_id, occurred_at, received_at, severity enum(`critical`,`error`,`warning`,`info`), kind enum(`error`,`alert`,`security_finding`,`log_match`,`event_bus`), service, environment, title, message_scrubbed, stack jsonb, attrs jsonb, fingerprint, issue_id, suppressed_by nullable | **Samples only** (first 5 + reservoir 20/fp/hour), not every event. Partitioned by day; unique(connector_id, external_id, received_day); idx(issue_id, occurred_at desc). Retention 30 days (partition drop) |
| `issue_counts_minutely` | issue_id, minute, count, suppressed_count | PK(issue_id, minute); powers sparklines and "spiking" sort; retention 30 days, hourly rollup kept 1 year |
| `issues` | id, fingerprint (unique), kind, title, service, environment, first_seen, last_seen, occurrences bigint, sources text[], status enum(`new`,`decoded`,`suppressed`,`acknowledged`,`resolved`,`regressed`), severity_max, known_issue_id nullable, decode_id nullable, assignee_user_id | idx(status, last_seen desc), idx(service) |
| `decodes` | id, issue_id, summary, probable_cause, impact, affected_code jsonb (repo, path, symbol, lines, chunk_id), related_commits jsonb, related_docs jsonb, similar_issue_ids uuid[], next_steps text[], confidence enum(`high`,`medium`,`low`), provider, model, tokens, created_at, fingerprint_version | Latest decode per issue linked via `issues.decode_id` |
| `known_issues` | id, title, description, explanation (Hub-generated plain-English explanation), source_text (pasted text or fetched Jira/Confluence body), jira_key nullable, reason enum(`known_bug`,`wont_fix`,`third_party`,`expected_noise`,`cannot_action`,`in_progress`), match jsonb (see § 8.10), scope (services/environments/sources), action enum(`suppress`,`label_only`), expires_at nullable, source enum(`manual`,`suggested`,`confluence`,`jira`,`pasted`), confluence_page_id, owner_user_id, ticket_url, created_at, hits bigint, last_hit_at | Suggested rules are inactive until a human approves |
| `known_issue_suggestions` | id, issue_ids uuid[], proposed_match jsonb, rationale, status enum(`pending`,`accepted`,`rejected`), created_at | Produced by the suggestion job |

### 6.5 Q&A, jobs, usage
| Table | Key fields | Notes |
|---|---|---|
| `qa_threads` | id, user_id, title, scope jsonb (repo_ids, include_signals, include_confluence), created_at | |
| `qa_messages` | id, thread_id, role enum(`user`,`assistant`), content, citations jsonb, provider, model, tokens, cached bool, feedback enum(`up`,`down`,null), created_at | |
| `answer_cache` | key (sha256 of normalized question + scope + index_version), answer, citations, created_at, hits | TTL 24h or until index_version changes |
| `jobs` | id, type, repo_id nullable, dedupe_key, payload jsonb, status enum(`queued`,`processing`,`done`,`failed`,`aborted`,`spend_blocked`,`pending_approval`,`needs_human`,`dead`), priority smallint, attempts, max_attempts (5), run_after, locked_by, locked_until, correlation_id, error, result jsonb, replayed_from, created_at, updated_at | idx(status, priority desc, run_after, id) where status='queued'; unique(dedupe_key) where status in (queued, processing) |
| `usage_events` | id, at, feature, provider_kind, model, input_tokens, output_tokens, cost_usd, latency_ms, cached bool, repo_id, user_id, job_id, issue_id, outcome enum(`ok`,`error`,`blocked`) | Partitioned by month; drives the ledger and analytics |
| `usage_rollups_hourly` | hour, feature, provider_kind, model, repo_id, calls, input_tokens, output_tokens, cost_usd, cached_calls | Rebuilt incrementally every 5 min |
| `savings_events` | id, at, kind enum(`triage_abort`,`known_issue_suppressed`,`decode_reused`,`answer_cache_hit`,`rename_rekey`), est_tokens_avoided, est_cost_avoided_usd, ref_id | Powers "saved" figures on the analytics page |
| `spend_limits` | scope enum(`global`,`feature`,`provider`,`repo`), scope_key, window enum(`day`,`month`), max_tokens, max_cost_usd, on_breach enum(`block`,`block_and_alert`) | 0/null = unlimited only with `allow_unlimited` flag set |

### 6.6 State management
- **Postgres**: all durable state above.
- **In memory**: grammar registry, compiled known-issue rules (reloaded on change via `LISTEN/NOTIFY`),
  per-connector rate limiters, LRU of recently seen fingerprints (10k entries) to skip DB reads on hot
  error storms.
- **Caches**: answer cache in Postgres (TTL 24h, invalidated by index_version bump on any chunk write);
  decode reuse by fingerprint (no TTL — invalidated when the linked code chunks change).
- **Git (repo)**: generated Markdown under `docs_path` is the canonical, reviewable copy of the Tree.

## 7. API Design

All endpoints under `/api/v1`, JSON, OpenAPI 3.1 spec served at `/api/v1/openapi.json`.
**Auth**: browser session cookie (`dth_session`, HttpOnly, Secure, SameSite=Lax) + CSRF token header for
mutating browser requests, or `Authorization: Bearer <PAT>`. **Error contract** (every error):
```json
{ "error": { "code": "STRING_CODE", "message": "human readable", "details": {}, "correlation_id": "..." } }
```
**Pagination**: cursor-based: `?limit=50&cursor=<opaque>` → `{ "items": [...], "next_cursor": "..." | null }`,
max limit 200. **Roles**: V=viewer, E=editor, A=admin, O=owner (each includes the ones before it).

### 7.1 Ingress (no session; verified per connector)
| Method & path | Verification | Response |
|---|---|---|
| `POST /hooks/github/{connector_id}` | `X-Hub-Signature-256` HMAC-SHA256, constant-time | `202 {"accepted":true,"job_id"}` · `200 {"accepted":false,"reason":"bot_loop_guard"\|"ignored_path"\|"untracked_repo"\|"untracked_branch"}` |
| `POST /hooks/gitlab/{connector_id}` | `X-Gitlab-Token` constant-time | same |
| `POST /hooks/sentry/{connector_id}` | `Sentry-Hook-Signature` HMAC-SHA256 | `202 {"accepted":n}` |
| `POST /hooks/pagerduty/{connector_id}` | `X-PagerDuty-Signature` (v1=HMAC-SHA256) | `202` |
| `POST /hooks/firehose/{connector_id}` | `X-Amz-Firehose-Access-Key` constant-time; responds with Firehose's required `{requestId, timestamp}` body | `200` |
| `POST /hooks/{datadog\|grafana\|alertmanager\|opsgenie\|splunk\|wiz\|gcp\|aws\|generic}/{connector_id}` | Per-connector bearer secret or basic auth configured in the source tool (these tools do not all sign payloads) | `202` |

Errors: `401 INVALID_SIGNATURE`, `400 MALFORMED_PAYLOAD`, `404 UNKNOWN_CONNECTOR`, `413 PAYLOAD_TOO_LARGE`
(> 5 MB; Firehose batches up to its configured buffer size are accepted), `429 RATE_LIMITED` (per-connector
limit, default 6,000 req/min — high because Firehose and alert tools batch; senders retry on 429). Ingress does no LLM or git I/O.

### 7.2 Auth & users
| Endpoint | Role | Purpose |
|---|---|---|
| `GET /auth/login` → OIDC redirect; `GET /auth/callback`; `POST /auth/logout` | – | Browser login |
| `GET /me` | V | `{id,email,name,role,repo_access:[...]}` |
| `GET/POST/DELETE /tokens` | V (own) | PAT management; POST returns `{token}` once |
| `GET /users`, `PATCH /users/{id}` `{role,disabled}` | A | User admin |
| `PUT /users/{id}/repo-access` `{repo_ids:[...],level}` | A | Repo ACL |
| `GET /audit?actor=&action=&since=` | A | Audit log |

### 7.3 Ask (Q&A)
**`POST /ask`** (V) — streams `text/event-stream` when `Accept: text/event-stream`, else JSON.
```json
Request:  { "question": "string (1..4000 chars)", "thread_id": "uuid|null",
            "scope": { "repo_ids": ["uuid"] , "include": ["code","docs","confluence","issues"] } }
Response: { "thread_id": "uuid", "message_id": "uuid", "answer": "markdown",
            "citations": [ { "n": 1, "type": "code|doc|confluence|issue", "title": "...",
                             "url": "...", "repo": "org/x", "path": "...", "lines": "10-42" } ],
            "cached": false, "usage": { "input_tokens": 0, "output_tokens": 0, "cost_usd": 0.0 } }
SSE events: `delta` {text}, `citation` {…}, `done` {message_id, usage}, `error` {error}
```
Errors: `400 QUESTION_EMPTY`, `403 SCOPE_FORBIDDEN` (repo outside user access), `402 SPEND_BLOCKED`,
`503 NO_LLM_ROUTE`. Rate limit: 30 asks/min per user.
Also: `GET /threads`, `GET /threads/{id}`, `DELETE /threads/{id}`,
`POST /messages/{id}/feedback` `{ "value": "up|down", "comment": "..." }`.

### 7.4 Docs: Tree, Palace, Library
| Endpoint | Role | Response |
|---|---|---|
| `GET /docs/tree?repo_id=&path=` | V | `{nodes:[{id,kind,title,path,has_children}]}` |
| `GET /docs/node/{id}` | V | `{title, markdown, source_url, updated_at, commit_sha, chunks:[...]}` |
| `GET /palace/entities?kind=&q=&repo_id=` | V | paginated entities |
| `GET /palace/entities/{id}/graph?depth=1..3&edge_kinds=` | V | `{nodes:[...],edges:[...]}` (ACL-filtered) |
| `GET /library/shelves` · `GET /library/shelves/{slug}` | V | shelves with items |
| `POST /library/shelves` · `PATCH/DELETE /library/shelves/{id}` · `POST /library/shelves/{id}/items` | E | curation |

### 7.5 Inbox (issues) & known issues
| Endpoint | Role | Notes |
|---|---|---|
| `GET /issues?status=&source=&service=&env=&severity=&q=&since=` | V | Paginated, sorted by last_seen desc |
| `GET /issues/{id}` | V | issue + latest decode + last 50 events + linked entities |
| `PATCH /issues/{id}` `{status, assignee_user_id}` | E | Acknowledge/resolve |
| `POST /issues/{id}/decode` `{force:false}` | E | Re-decode (spend-guarded) → `202 {job_id}` |
| `POST /issues/{id}/mark-known` `{title, reason, match_override?, expires_at?}` | E | Creates a known-issue rule from this issue's fingerprint |
| `GET /known-issues` · `POST` · `PATCH /known-issues/{id}` · `DELETE` | V / E | CRUD |
| `POST /known-issues/from-text` `{text, services?:[...]}` | E | Paste an incident note/runbook/ticket text → `{explanation, proposed_match, matching_issues_last_7d}`; nothing is active until saved |
| `POST /known-issues/from-link` `{url}` | E | Jira issue or Confluence page URL → fetched via the connector → same response as from-text |
| `POST /known-issues/test` `{match}` | E | `{would_match_last_7d: n, sample_issue_ids:[...]}` — dry-run a rule before enabling |
| `GET /known-issues/suggestions` · `POST /known-issues/suggestions/{id}/accept\|reject` | E | Auto-suggestions |

### 7.6 Repos, connectors, providers, spend
| Endpoint | Role | Notes |
|---|---|---|
| `GET /repos` · `PATCH /repos/{id}` `{tracked_branch, docs_path, push_mode, service_name, owners, enabled}` | V / A | |
| `POST /repos/{id}/dry-run` `{ref:"HEAD"}` | E | `{triage, would_write:[...], chunks, estimated_tokens, estimated_cost_usd}` — no side effects |
| `POST /repos/{id}/import` `{source_paths:[...]}` | A | `202 {job_id, files_discovered}` |
| `GET /connectors` · `POST /connectors` `{type,name,config,credentials}` · `PATCH` · `DELETE` | A | Creds write-only |
| `POST /connectors/{id}/test` | A | `{ok, checks:[{name, ok, detail}]}` — read-only permission probe |
| `POST /connectors/{id}/sync` | A | Trigger an immediate poll |
| `GET /providers` · `POST /providers` · `PATCH` · `DELETE` · `POST /providers/{id}/test` | A | BYO LLM keys |
| `GET /routes` · `PUT /routes/{feature}` `{provider_id, model, max_output_tokens, fallback...}` | A | Feature routing |
| `GET /spend/limits` · `PUT /spend/limits` | A | Ceilings |
| `POST /reindex` `{embedding_provider_id, model, acknowledge_destructive:true}` | O | `202 {job_id, chunks_to_reembed, estimated_tokens}` |

### 7.7 Jobs, activity, analytics
| Endpoint | Role | Notes |
|---|---|---|
| `GET /jobs?type=&status=&repo_id=` · `GET /jobs/{id}` | V | Job detail includes triage, chunk counts, spend, docs_ref |
| `POST /jobs/{id}/retry` `{override_ceiling:false}` | A | `403 CEILING_OVERRIDE_REQUIRED` for spend_blocked without override |
| `GET /activity?types=&since=` | V | Feed: pushes processed, docs landed, PRs, issues decoded, rules hit, connector syncs, admin actions |
| `GET /analytics/usage?group_by=feature\|provider\|model\|repo\|user&from=&to=&granularity=hour\|day` | V (own) / A (all) | `{series:[{key, points:[{t, calls, tokens, cost_usd}]}], totals}` |
| `GET /analytics/savings?from=&to=` | V | `{by_kind:[{kind, events, tokens_avoided, cost_avoided_usd}], total}` |
| `GET /analytics/pipeline?from=&to=` | V | job counts by status, p50/p95 latency, triage abort rate, index freshness per repo |
| `GET /analytics/connectors` | A | health, last sync, events/day per connector |

### 7.8 Health
`GET /healthz` → `{"status":"ok"}` · `GET /readyz` → `{"status":"ready","checks":{"db":"ok","llm_routes":"ok","vector":"ok"}}` or `503` · `GET /metrics` (Prometheus, bind to internal port 9090).

### 7.9 Internal contracts
**DocGen contract v2** (carried from the original plan's v1, extended additively): task JSON
`{contract_version:"2.0", job_id, repo, commit_sha, chunks_to_generate:[{chunk_id, symbol, file_path,
change_type, target_doc_path}], context:{...}, output_path}` → result JSON `{contract_version, status,
error, docs:[{path, content, chunk_id, symbol}], usage:{input_tokens, output_tokens, provider, model},
extensions:{}}`. Used by the `external_cli` doc-gen route (any headless agent CLI).

**Decode contract** (LLM must return JSON): `{summary, probable_cause, impact, affected_code:[{chunk_id,
reason}], next_steps:[...], confidence:"high|medium|low", is_actionable:bool, suggest_known_issue:bool}`.
Validated; one repair-retry; then stored as `confidence: low` with raw text.

**Answer contract**: answer Markdown where every factual sentence ends with `[n]` referencing a supplied
chunk; the server drops citations to chunk IDs that were not supplied (prevents fabricated sources).

## 8. Logic & Algorithms

### 8.1 Triage (carried forward)
Deterministic first: skip ignored paths (default list: lockfiles, vendored/generated output, minified
assets, CI config), the generated-docs path, and bot-authored pushes; parse old/new with Tree-sitter; strip
comment nodes and docstring-only statements, collapse string-literal contents, ignore whitespace while
keeping nesting; if normalized token streams are equal → `ABORT`, else `PROCEED` with a reason listing
added/removed definitions and type, signature, or body changes. String literals are *not* cosmetic when
they are route/decorator/annotation arguments, import paths, or environment-variable names (renaming the
env var an app reads or the path it serves is a structural change). Test files (default `IndexOnly`
globs) are indexed for search but never get generated documentation.
Dependency manifests (`go.mod`, `package.json`, `pom.xml`, `Cargo.toml`, `requirements.txt`,
`pyproject.toml`): `PROCEED` only on a change of the leading semver component of an existing dependency
(Go `/vN` suffix treated as the major); everything else `ABORT`. Binary → `ABORT`. Deleted file →
`PROCEED` (remove chunks). No grammar → one LLM triage call (route `triage`), ambiguity → `PROCEED`.
Complexity O(nodes) per file, no network in the common case.

### 8.2 Chunking & stable IDs (carried forward, scope added)
Chunk ID = `sha256(scope + "::" + path + "::" + symbol_or_heading_slug)[:16]` where `scope` is the repo
full name, `confluence:<space>`, or `issue`. Code: one chunk per function/method/class/type node (node-type
map per language), with directly preceding doc comments and decorators/attributes included; symbols are
qualified (`Class.method`, `Type.Method` for Go receivers, `impl Trait for Type` in Rust) and overloads are
disambiguated by parameter list. Class/impl/module chunks are *outlines* (members shown as signatures) so
member bodies are embedded once and a method edit does not mark its class changed. Top-level code outside
definitions forms one `__module__` chunk. Files without a grammar become one whole-file text chunk. Docs: H2/H3 sections, split past ~900 tokens with 50-token overlap. Renames with
similarity ≥ 90 and empty AST diff: re-key chunks (copy vectors to new IDs, delete old) — no re-embedding.

### 8.3 Manifest diff (carried forward)
New chunk set vs stored: new ID → added; same ID, different content_hash → changed; stored for a changed
file but absent → removed (soft delete, GC after 14 days). `generated_doc` supersedes `imported_doc` on the
same ID; an import never overwrites a generated chunk.

### 8.4 Doc generation and the Tree
One doc file per source file: `<docs_path>/<source_path>.md`. Each symbol is a section headed `## <symbol>`
preceded by `<!-- dth:chunk <chunk_id> -->`. The Hub owns assembly: it fetches the current doc file, replaces
sections for changed chunks, inserts added ones in source order, removes removed ones, and preserves any
section wrapped in `<!-- dth:human -->…<!-- dth:end -->` untouched (this is how teams **extend** generated
docs by hand without losing edits). Directory index files (`README.md` per directory under docs_path) list
child files with one-line summaries and are regenerated only when a child's summary changes. Result:
`doc_nodes` mirrors the tree for the UI.

### 8.5 The Palace (knowledge graph) — extraction rules
Built deterministically from ASTs and config, not by LLM, so it is cheap and reproducible:
| Entity/edge | Extraction |
|---|---|
| `repo`→`module`→`file`→`symbol` (`contains`) | Directory structure + chunker |
| `symbol` `calls` `symbol` | Call expressions resolved same-file and one hop via imports (§ 8.6) |
| `endpoint` (`exposes`) | Framework route patterns: Go `http.HandleFunc`/chi/gin/echo, Java Spring `@GetMapping` etc., Python FastAPI/Flask decorators, Express/Nest routes, Rust axum/actix — pattern table per language, extensible via config |
| `env_var` (`reads_env`) | `os.Getenv`, `System.getenv`, `os.environ[...]`/`getenv`, `process.env.X`, `std::env::var` |
| `dependency` (`depends_on`) | Dependency manifests |
| `datastore`, `queue_topic` | Connection-string env var names and client constructors (pattern table: postgres, mysql, redis, kafka, sqs, pubsub, s3, gcs, dynamodb) |
| `service` (`deployed_as`) | `service_map` + Dockerfile/Helm/K8s manifests/`serverless.yml` names in repo |
| `issue` `raises` from `symbol` | Decoded issues' affected_code |
| `confluence_page` `documented_in`/`runbook_for` | Page mentions of service/repo/endpoint names (exact match on entity keys) |
| `owned_by` | CODEOWNERS file + `repos.owners` |
Edges carry `evidence` (file, line, commit). Removed code soft-deletes its edges in the same job.
Graph queries use recursive CTEs bounded by depth ≤ 3 and 500 nodes.

### 8.6 Scoped context (carried forward)
Changed symbol bodies + one-hop same-file callers/callees (signatures only) + referenced types + import
block + one-hop cross-file signatures via import statements + top-3 Confluence sections linked to the
service + active known issues for the service + optional `ContextProvider` outputs. Over budget → drop
cross-file signatures, then same-file signatures, then Confluence/provider output; never the changed bodies.
`context_tokens_per_job` metric proves the reduction.

### 8.7 The Library — shelf assignment
Each doc section, entity, Confluence page, and known issue is assigned to shelves by rules (entity kind,
path globs such as `**/adr/**` → Decisions, Confluence labels, tags). Rules are data (`library_shelves.rule`),
evaluated on every write — no LLM. Curators can pin items and create shelves; curated shelves are never
overwritten by rules. Each shelf page shows items grouped by repo/service with freshness (last updated
commit/date).

### 8.8 Signal normalization & redaction
Every adapter maps to `SignalEvent{source, external_id, occurred_at, severity, kind, service, environment,
title, message, stack[], attrs}`. Severity mapping table per source (e.g. PagerDuty `high`→`error`,
Alertmanager `critical`→`critical`, Wiz `CRITICAL`→`critical`). Service resolution order: explicit tag
(`service`, `dd.service`, k8s label `app`) → `service_map` source patterns (log group, GCP resource labels)
→ `unknown`.
**Two layers, because you bring your own LLM:**
- **Secret scrubbing — always on, not configurable off**: AWS access keys (`AKIA[0-9A-Z]{16}`), GCP API
  keys, JWTs, bearer tokens, private-key blocks, passwords in URLs (`://user:pass@`), connection strings.
  Rationale: a credential copied into a prompt, a stored sample, or an answer is a security incident
  regardless of whose LLM it is, and scrubbing also removes high-entropy noise that wastes tokens.
- **PII redaction — per LLM provider toggle, default OFF**: emails, phone numbers, IPv4/IPv6, card numbers
  (Luhn-checked), plus operator-defined patterns (customer IDs, account numbers). Because the keys and the
  model contract are yours, you decide per provider (e.g. OFF for Bedrock/Vertex/Ollama in your own
  account, ON for an external API). Changing it is admin-only and audit-logged.
Replacements are typed placeholders (`<EMAIL>`, `<AWS_KEY>`) and are applied before storage of samples,
before fingerprinting (so the same error for different customers groups together), and before any LLM call.

### 8.9 Fingerprinting & grouping
1. Normalize message: lowercase; replace UUIDs, hex ≥ 8 chars, numbers, quoted strings, emails, IPs, paths
   with digits, timestamps with placeholders; collapse whitespace; truncate 512 chars.
2. Error events: `fp = sha256(source_family + service + exception_type + normalized_message +
   top-5 in-app stack frames as "module:function" (no line numbers))`. If the source provides its own group
   ID (Sentry issue ID, GCP Error Reporting group, Datadog error tracking issue), use `source + group_id`
   and store the normalized fp as `alt_fingerprint` for cross-source matching.
3. Alerts: `fp = sha256(source + rule/monitor/alarm id + service + environment)` — each firing of the same
   alarm is one issue.
4. Security findings (Wiz): `fp = sha256("wiz" + control/rule id + resource id)`.
5. Upsert `issues` on fp (`occurrences += 1`, `last_seen`, severity max); status `resolved`→`regressed` on a
   new occurrence.
Hot path (§ 4.5): each replica keeps an in-memory map fp → {count, first sample, reservoir} for a 1-second
window and an LRU of fp → issue_id (100k entries); each flush does one multi-row `INSERT … ON CONFLICT
(fingerprint) DO UPDATE SET occurrences = issues.occurrences + EXCLUDED.occurrences`. Known-issue matching
runs before aggregation, so suppressed events cost only a counter increment. Complexity O(message length)
per event; Postgres writes O(distinct fingerprints per second). Backpressure: if the flush falls behind
(> 3 windows buffered), the Pub/Sub consumer stops pulling and the Firehose endpoint returns 503 so AWS
retries — no event loss, bounded memory.

### 8.10 Known-issue matching (the usage saver)
Rule `match` object (all present fields must match — AND):
```json
{ "fingerprints": ["..."], "message_regex": "timeout .* payments-gw", "sources": ["sentry"],
  "services": ["checkout"], "environments": ["staging"], "attrs": {"http.status": "503"},
  "min_severity": "warning", "max_severity": "error" }
```
Compiled to an in-memory matcher: exact fingerprint map first (O(1)), then per-service rule lists, then
regex (RE2 via Go `regexp`, linear time — no catastrophic backtracking). Evaluated **before** any LLM call
and before the issue is enqueued for decode. On match: event `suppressed_by = rule_id`, issue status
`suppressed`, `known_issues.hits++`, `savings_events` row with the estimated decode cost that was avoided
(average of the last 100 decodes for that route, or the route's estimate). Rules with `expires_at` auto-
disable (a "mute for 7 days" button). `action: label_only` tags without suppressing.
**Auto-suggestions** (daily job, route `suggest`, spend-guarded): issues with ≥ 50 occurrences, ≥ 3 days
old, never acknowledged, and decoded `is_actionable: false` or `suggest_known_issue: true` are proposed as
rules; a human accepts or rejects. Nothing is suppressed automatically without a human.
**From the Inbox list**: "Mark as known" on any row (or multi-select rows) pre-fills a rule with those
issues' fingerprints, service, and environment; the user picks a reason and optional expiry.
**From pasted text, a Jira issue, or a Confluence page** (route `suggest`, spend-guarded):
1. Fetch the text (Jira REST / Confluence REST via the connector) or take the pasted text (max 50k chars,
   secrets scrubbed).
2. Deterministic pre-pass: extract exception class names, error codes, quoted error messages, service and
   endpoint names that exist as Palace entities, and log-group/metric names.
3. Candidate search: find existing Issues from the last 30 days whose fingerprint message, exception type,
   or service matches the extracted terms (full-text + vector similarity on decode summaries).
4. One LLM call returns `{explanation, reason, proposed_match, confidence}` given the text and up to 20
   candidate issues.
5. The UI shows the explanation, the proposed rule, and exactly which recent issues it would have matched
   (via `/known-issues/test`); nothing is active until a human saves it.
Jira issues / Confluence pages with the configured label (default `known-issue`) are re-checked on each sync;
a Jira issue moving to Done flips the rule's `action` to `label_only` and marks it "fixed upstream — verify",
so a fixed bug's errors are no longer hidden.

### 8.11 Decode
Context (budget 12k tokens): redacted event sample (3 most recent), stack frames mapped to code chunks via
exact `path+symbol` lookup then vector search restricted to the service's repos, doc sections for those
chunks, commits in the last 7 days touching those files (`CodeHost.ListCommits(path, since)`), top-3
similar decoded issues (vector similarity on decode summaries, threshold 0.85), linked Confluence runbooks.
Output validated against the decode contract. Re-decode triggers: user request, or the affected code chunks
changed since the decode (lazy — on next occurrence). Rate: decode queue concurrency 4 by default.

### 8.12 RAG retrieval & answering
1. Normalize question (trim, collapse whitespace, lowercase for cache key only).
2. Cache lookup → hit → return, record `answer_cache_hit` savings.
3. Embed question (route `embedding`) — spend-guarded, tiny.
4. Candidates: pgvector cosine top 40 filtered by allowed repo_ids and included sources; full-text
   `websearch_to_tsquery` top 40 with same filters.
5. Reciprocal Rank Fusion: `score = Σ 1/(60 + rank_i)`; take top 20.
6. Graph expansion: for the top 5 chunks' entities, add 1-hop neighbours' summary chunks (max 10), ACL-filtered.
7. Budget packing to route's context budget (default 16k tokens) by score, keeping ≤ 3 chunks per file.
8. LLM answer with citation contract; drop invalid citations; if zero valid citations, respond
   "I could not find this in the connected sources" rather than an uncited answer.
ACL is enforced in SQL (step 4/6) — never post-filtered after the LLM sees content.

### 8.13 Spend guard (extended)
Before every paid call (docgen, decode, qa, triage, suggest, embedding): estimate tokens (context chars/4 +
max_output_tokens) → evaluate every applicable limit (`global`, `feature`, `provider`, `repo`) over its
window from `usage_events` sums (cached per minute) → any breach → `spend_blocked` (jobs) or `402
SPEND_BLOCKED` (API), ERROR log, metric, optional alert webhook. Actual usage written after the call.
Providers or adapters that report no usage are recorded as estimate=actual and flagged "unguarded" in the
UI; running them requires `allow_unreported_usage`. Limits of 0 require `allow_unlimited`.
`dry-run` stops before any paid call and returns the estimate.

### 8.14 Model routing & BYO keys
Each feature has one route (provider + model + max tokens) and an optional fallback. On a
`TransientError` (429/5xx/timeout) the call retries with jittered backoff (1s, 2s, 4s; max 3) then tries the
fallback route once, still through the spend guard. Keys are decrypted just-in-time per call and never
logged. `POST /providers/{id}/test` performs a 1-token call and reports latency.

### 8.15 Queue & concurrency
Claim: `UPDATE jobs SET status='processing', locked_by=$w, locked_until=now()+ttl WHERE id = (SELECT id
FROM jobs WHERE status='queued' AND run_after<=now() AND (repo_id IS NULL OR pg_try_advisory_xact_lock(...)
…) ORDER BY priority DESC, id LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING *`. Per-repo ordering: a
`code_push` job is claimable only if no earlier queued/processing `code_push` job exists for that repo
(enforced in the claim predicate). Lease heartbeat every ttl/3; expired leases are reclaimed by the
scheduler. Retry: exponential backoff 1s·2ⁿ with ±20% jitter, max 5, then `dead`. `Retry-After` from
providers overrides backoff without consuming an attempt. Concurrency caps per job type (defaults:
code_push 4, decode_issue 4, knowledge_sync 2, signal_batch 8, reindex 1). **Delivery semantics**:
at-least-once; every handler is idempotent (upserts keyed by chunk_id / fingerprint / external_id;
docs PR branch named by job ID).

### 8.16 Connector sync (polling sources)
High-volume log sources are **push**, not poll: CloudWatch Logs subscription filters (default pattern
`?ERROR ?Exception ?Traceback ?FATAL`) → Firehose → `/hooks/firehose/{id}`; GCP Log Router sink
(`severity>=ERROR`) → Pub/Sub → pull subscriber. Polling remains for low-volume APIs, each with a cursor
per stream: CloudWatch Logs `FilterLogEvents` (only for log groups without a subscription, e.g. local
installs); CloudWatch Alarms via
`DescribeAlarmHistory` (or EventBridge push); GCP Logging `entries.list` with `severity>=ERROR AND
timestamp>cursor`; GCP Error Reporting `groupStats.list`; Splunk runs configured saved searches via
`/services/search/jobs` (`earliest_time=cursor`); Wiz GraphQL `issues` query filtered by `updatedAt >
cursor`; Confluence REST v2 pages by space with `last-modified > cursor` (CQL); Jira issues by JQL
`project in (...) AND updated > cursor`. Cursors advance only after
the batch is committed (at-least-once, idempotent on external_id). Per-connector rate limits respect
documented API limits and `Retry-After`. Poll interval default 60s (signals), 15 min (Confluence).

### 8.17 Push modes & PR lifecycle (carried forward)
`pr_auto_merge` default; `direct` with `on_reject` fallback; `pr_with_approver` where the approver's own
review satisfies branch protection and the bot merges — no code path ever accepts a second user's
credentials. Conflict → auto-rebase → on failure requeue; stale > 72h → close + requeue; invalid approver
→ fallback or `needs_human`; newer job covering the same chunks supersedes an older PR.

### 8.18 Zero-downtime reindex (carried forward)
New embedding model → create `chunks_embed_v{n+1}` column/table (pgvector) or collection (Qdrant) → dual-
write live updates → re-embed all live chunks → atomic swap (alias/view swap in one transaction) → drop old.
Startup refuses to run if the configured embedding model differs from `embedding_lock` without a reindex.

### 8.19 `dth up` bootstrap
Detect Docker → write default config → start Postgres(+Ollama if local embeddings chosen) → wait healthy
(180s) → run migrations → create owner account (local mode: generated password printed once; OIDC optional
locally) → open browser to the setup wizard (connect GitHub via App Manifest one-click, add an LLM
provider key, pick repos) → reachability probe decides webhook vs polling → first dry-run shows estimated
cost → ready. Idempotent; re-running is the health check and upgrade path.

### 8.20 Event-platform inspection (DLQs, lag, consumer errors)
All inspectors are read-only and produce `SignalEvent{kind: event_bus}` so they flow through the same
fingerprint → known-issue → decode path as errors and alerts.
| Platform | Lag / backlog signal | DLQ signal | Read-only mechanics |
|---|---|---|---|
| Kafka / MSK / Confluent | Consumer-group lag per partition via admin `ListOffsets` + `OffsetFetch` (kadm) | Configured DLQ topics (default pattern `*.dlq`, `*-dlq`, `*.DLT`) read by the Hub's **own** consumer group from latest offset; error class taken from headers (`x-exception-class`, `kafka_dlt-exception-fqcn`, configurable) | Never commits offsets for application groups; own group only |
| SQS | `ApproximateNumberOfMessagesVisible`, `ApproximateAgeOfOldestMessage` (CloudWatch) on source queues | Same metrics on DLQs discovered via each queue's `RedrivePolicy`; **opt-in** peek (`ReceiveMessage` with visibility timeout 0, no delete) for body/attribute samples | Peek is off by default because a receive increments `ApproximateReceiveCount` |
| SNS | `NumberOfNotificationsFailed` per topic | Subscription redrive DLQs (SQS) → SQS inspector | Metrics only |
| EventBridge | `FailedInvocations`, `ThrottledRules` per rule | Target DLQs (SQS) → SQS inspector | Metrics only |
| Kinesis | `GetRecords.IteratorAgeMilliseconds`, `ReadProvisionedThroughputExceeded`; Lambda consumers' `IteratorAge` | Lambda on-failure destinations (SQS/SNS) → SQS/SNS inspectors | Metrics only |
| GCP Pub/Sub | `subscription/num_undelivered_messages`, `oldest_unacked_message_age` | Dead-letter topics read via a **Hub-owned subscription** created by the `readonly-roles` Terraform module | Hub never acks application subscriptions |
| RabbitMQ | Queue `messages_ready`, `messages_unacknowledged`, consumer count (management API) | Queues bound to dead-letter exchanges (`x-dead-letter-exchange`) — depth and growth; optional `get` with `ackmode=reject_requeue_true` peek, off by default | Management API `monitoring` tag user |

**Issue rules** (per consumer group / queue / subscription, thresholds editable in the UI):
- Lag/backlog: `lag > max(1,000, 5 × p95 of the last 7 days)` **or** oldest-message age > 5 min, sustained
  for 5 min → Issue `fp = sha256(bus + resource + "lag")`; auto-resolves after 15 min below threshold.
- DLQ: depth increased in the last poll → Issue per `(bus, dlq, error class)`; the message sample's
  error class/reason header drives the fingerprint so different failure causes become different issues.
- Consumer error logs arrive through normal log ingest (§ 4.5); the Palace links the consumer group /
  subscription to its service (`subscribes` edge), so a DLQ issue, a lag issue, and the consuming service's
  error logs appear together on one Issue page with the producing service (`publishes` edge).
- Poll intervals: lag 30s, DLQ 60s; message bodies are secret-scrubbed and truncated to 8 KB before storage.

## 9. Error Handling Strategy

**Taxonomy**: `ValidationError` (400, never retried) · `AuthError` (401/403) · `NotFoundError` (404) ·
`ConflictError` (409) · `SpendBlockedError` (402 in API, `spend_blocked` in jobs) · `TransientError`
(retried; upstream 429/5xx/timeouts/DB serialization failures) · `PermanentError` (job `failed` → `dead`
after attempts; bad credentials, contract violations after repair, protected branch after fallbacks) ·
`InternalError` (500, bug).
**Rules**: adapters return typed errors, the core decides retry vs fail by type (never string matching);
API maps types to the error contract in § 7; every 5xx carries `correlation_id`; users see a one-line
message, logs carry full context (upstream status, body excerpt ≤ 2 KB with redaction applied).
**Fallbacks**: LLM route down → fallback route → job retries → `dead` visible in Activity; vector search
down → Q&A degrades to full-text only with a banner "semantic search unavailable"; connector failing →
health `failing`, exponential poll backoff up to 30 min, surfaced on Connectors page and `/readyz` stays
ready (one connector never takes the Hub down); Postgres down → `/readyz` 503, webhooks return 503 so the
sender retries (GitHub/GitLab/PagerDuty/Sentry all retry on 5xx).
**Dead letter**: `jobs.status='dead'` listed under Activity → Failed; `POST /jobs/{id}/retry` replays with a
fresh attempt count.

## 10. Testing Strategy

### Unit Tests
- Coverage ≥ 85% lines for `internal/core/**`, ≥ 75% for `internal/api`, `internal/queue`, `internal/auth`.
- Must cover: triage per language + edge cases, chunk ID stability, manifest diff, doc section assembly with
  human-preserved blocks, palace extraction per language pattern table, library rule evaluation,
  redaction patterns (positive and negative samples), fingerprint stability (same error, different IDs →
  same fp), known-issue matcher (every match field, expiry, label_only), RRF fusion, citation filtering,
  spend guard across every limit scope, model-route fallback, PR state machine, RBAC matrix.
- All ports mocked (`testify/mock`); naming `TestFingerprint_DifferentUUIDs_SameFingerprint`.
- Frontend: Vitest + RTL for every page's loading/empty/error/data states.

### Integration Tests
- `testcontainers-go`: Postgres 16 + pgvector (migrations, queue claim under concurrency, HNSW search,
  ACL-filtered retrieval), Qdrant (vector adapter), LocalStack (CloudWatch Logs + STS).
- HTTP mock servers built from recorded fixtures for GitHub, GitLab, Sentry, PagerDuty, Opsgenie, Datadog,
  Grafana, Alertmanager, Wiz GraphQL, Splunk REST, Confluence REST, GCP Logging, and every LLM provider.
- **Adapter parity suites** (mandatory, never weakened): every `CodeHost`, `VectorIndex`, `LLM`,
  `Embedder`, and `SignalSource` adapter runs the same table-driven suite for its port.
- **Signal fixture suite**: for every source, a real-world payload fixture must normalize to an expected
  `SignalEvent` and fingerprint.

### End-to-End Tests (Playwright)
- Cold start: empty dir → `dth up` → setup wizard → connect mock GitHub → push fixture commit → doc PR
  opened/merged → question answered with a citation to the new doc.
- Error flow: POST a Sentry fixture → issue appears in Inbox decoded → "Mark as known" → second identical
  event is suppressed and appears in Analytics savings.
- Spend: limit below estimate → `spend_blocked`, no provider call recorded by the mock.
- RBAC: viewer without access to repo B cannot see B's chunks in answers, graph, or docs.
- Runs against real GitHub/GitLab test projects nightly when credentials are present in CI secrets.

### Contract Tests
- `dth adapter-test <command>` for external doc-gen CLIs (contract v2).
- JSON schema tests for decode and answer contracts against every LLM adapter's mock.

### Performance Tests (k6)
- Signal storm: 20,000 error events/s for 10 min across Firehose, Pub/Sub, and webhooks (3 api replicas,
  2 vCPU / 4 GB each) with 2,000 distinct fingerprints → ingress p99 < 300 ms, zero lost events,
  Postgres writes ≤ 2,100 row-upserts/s, decode jobs = number of distinct new fingerprints.
- Incident spike: one fingerprint at 50,000 events/s for 60 s → exactly 1 issue, counts accurate to ±0,
  memory bounded.
- Push burst: 40 commits across 5 repos with a 90s stub LLM → queue drains in < 30 min at docgen
  concurrency 4; concurrency cap never exceeded.
- Q&A: 50 concurrent users, 1 ask/10s each, stub LLM → retrieval stage p95 < 400 ms at 250k chunks.
- Results recorded as measured values on documented hardware, not guarantees.

### Quality Evaluations (LLM features)
- Golden set of 50 questions over fixture repos with expected cited files; CI reports citation precision
  and recall; regression > 10 points fails the build.
- Golden set of 30 error fixtures with expected affected file; decode "affected code" top-1 accuracy tracked.

### Test Data Strategy
Fixture repos as `.git` bundles per language; signal payload fixtures per source; factories for DB rows;
every integration test uses a fresh schema; no live third-party calls in PR CI.

## 11. Observability & Logging

- **Logs**: slog JSON; fields `correlation_id`, `job_id`, `job_type`, `repo`, `connector`, `issue_id`,
  `user_id`, `component`. DEBUG per-chunk/per-event detail; INFO job start/finish, triage decision, decode
  stored, rule matched; WARN retries, connector degraded, validation rejections; ERROR dead jobs, spend
  blocks, adapter auth failures. Secrets and unredacted messages are never logged.
- **Metrics** (Prometheus): `dth_http_requests_total{route,code}`, `dth_http_duration_seconds`,
  `dth_ingress_events_total{connector,accepted}`, `dth_jobs_total{type,status}`,
  `dth_job_duration_seconds{type}`, `dth_queue_depth{type}`, `dth_llm_calls_total{feature,provider,model,outcome}`,
  `dth_llm_tokens_total{feature,provider,direction}`, `dth_llm_cost_usd_total{feature,provider}`,
  `dth_spend_utilization_ratio{scope}`, `dth_spend_blocked_total{feature}`,
  `dth_savings_total{kind}`, `dth_issues_new_total{source}`, `dth_events_suppressed_total{rule}`,
  `dth_context_tokens_per_job` (histogram), `dth_connector_health{connector}` (0/1/2),
  `dth_index_freshness_seconds{repo}`, `dth_retrieval_duration_seconds`, `dth_grammar_missing_total{ext}`.
- **Tracing**: OTel spans for ingress → job → each port call; OTLP exporter off by default.
- **Alerting thresholds** (documented for the operator): spend utilization > 0.8; queue depth > 500 for
  10 min; any connector `failing` > 30 min; job failure rate > 5%/h; index freshness > 1h for a repo with
  pushes; `/readyz` failing > 2 min.
- **In-product**: the Activity & Analytics page is built from the same tables, so what operators see in
  Grafana and in the UI agree.

## 12. Security Considerations

- **Authentication**: OIDC (auth-code + PKCE) for users; sessions 12h idle / 7d absolute, rotated on login;
  PATs with optional expiry, hashed at rest. Local-only mode (no OIDC) supports a single owner password
  (argon2id) and binds to `127.0.0.1` by default.
- **Authorization**: RBAC (owner/admin/editor/viewer) enforced in API middleware; repo-level ACL enforced
  in SQL for every read of chunks, docs, graph, issues (issues inherit ACL from their mapped repo; unmapped
  issues visible to all viewers by default, configurable).
- **Cloud access is read-only by construction**: Terraform ships least-privilege IAM policies
  (CloudWatch Logs read/filter, `cloudwatch:DescribeAlarmHistory`, no write actions); GCP roles
  `roles/logging.viewer`, `roles/errorreporting.viewer`, `roles/monitoring.viewer`; cross-account access via
  STS AssumeRole with an external ID. Connector test reports any excess permission it detects as a warning.
- **Secrets**: connector credentials and LLM keys encrypted with AES-256-GCM data keys wrapped by KMS
  (cloud) or a local key file (`0600`); never returned by the API, never logged; bootstrap secrets from env
  or Secrets Manager/Secret Manager.
- **Data sent to LLMs**: redaction on by default (§ 8.8); per-connector toggle "never send to LLM" (e.g.
  keep Wiz findings out of third-party models); provider allow-list per feature (e.g. decode may use only
  Bedrock/Vertex/Ollama for data residency).
- **Webhook ingress**: signature/secret verification before parsing; 5 MB cap; per-connector rate limit;
  replay protection by idempotency key and timestamp window where the source provides one.
- **Repo writes**: every write path validated to be under `docs_path` before calling the git host API;
  the bot-loop guard prevents self-triggered loops.
- **Web security**: CSP (`default-src 'self'`), CSRF tokens on cookie-auth mutations, HttpOnly/Secure
  cookies, rendered Markdown sanitized (rehype-sanitize), no inline scripts.
- **Prompt-injection hardening**: retrieved content is passed as quoted data blocks; the system prompt
  forbids following instructions inside them; answers can only cite supplied chunks; the Hub never executes
  model output (no tool calls against cloud APIs).
- **Transport**: TLS 1.2+ at ALB/Cloud Run/ingress; Postgres connections require TLS in cloud.
- **Supply chain**: `govulncheck`, `npm audit`, Trivy image scan, pinned base image digests, SBOM
  (Syft) attached to releases.
- **Audit**: every admin action, connector change, rule change, redaction toggle, and ceiling override is
  written to `audit_log`.

## 13. File & Directory Structure

```
DocTheRepo/
├── cmd/
│   ├── hub/main.go                      # Server entrypoint (--role=api,worker,scheduler)
│   └── dth/main.go                      # CLI entrypoint (Cobra root)
├── internal/
│   ├── config/                          # Bootstrap YAML + env loading and validation
│   ├── store/                           # pgx pool, migrations runner, repositories
│   │   ├── queries/                     # sqlc .sql query files
│   │   ├── gen/                         # sqlc generated code (committed)
│   │   └── storetest/                   # Test helper: real Postgres+pgvector via testcontainers, DB per test
│   ├── queue/                           # Postgres job queue, worker pool, per-repo ordering
│   ├── scheduler/                       # Leader-elected periodic tasks
│   ├── auth/                            # OIDC, sessions, PATs, RBAC, repo ACL
│   ├── secrets/                         # Envelope encryption + KeyEncrypter interface
│   ├── observability/                   # slog, metrics, tracing, audit writer
│   ├── api/                             # chi router, handlers, DTOs, SSE, ingress, UI embed
│   ├── ports/                           # Port interfaces + domain types (no I/O)
│   ├── core/
│   │   ├── triage/                      # AST-diff classification
│   │   ├── grammars/                    # Tree-sitter registry + node-type maps
│   │   ├── chunker/                     # Code/doc chunking, stable IDs, rename detection
│   │   ├── manifest/                    # Chunk delta computation
│   │   ├── docassembly/                 # Doc file section assembly, human-block preservation
│   │   ├── scopedcontext/               # Minimal context assembly + drop order
│   │   ├── spendguard/                  # Estimates and multi-scope ceilings
│   │   ├── palace/                      # Entity/edge extraction pattern tables
│   │   ├── library/                     # Shelf rule evaluation
│   │   ├── signals/                     # Normalization helpers, scrubbing, redaction, fingerprinting
│   │   ├── aggregate/                   # 1s fingerprint aggregation, reservoir sampling, flush
│   │   ├── knownissues/                 # Rule compiler and matcher
│   │   ├── decode/                      # Decode context + contract validation
│   │   ├── rag/                         # Retrieval fusion, packing, citation contract
│   │   └── pipeline/                    # Job handlers orchestrating ports
│   ├── adapters/
│   │   ├── codehost/github/             # GitHub App adapter
│   │   ├── codehost/gitlab/             # GitLab adapter (SaaS + self-managed)
│   │   ├── push/direct/                 # Direct commit landing
│   │   ├── push/prautomerge/            # Default: PR + auto-merge
│   │   ├── push/prapprover/             # PR + human approver
│   │   ├── push/lifecycle/              # Shared PR lifecycle sweep logic
│   │   ├── llm/anthropic/               # Anthropic Messages API
│   │   ├── llm/openai/                  # OpenAI API
│   │   ├── llm/azureopenai/             # Azure OpenAI deployments
│   │   ├── llm/bedrock/                 # AWS Bedrock Converse API
│   │   ├── llm/vertex/                  # Google Vertex AI
│   │   ├── llm/openaicompat/            # Any OpenAI-compatible endpoint incl. Ollama
│   │   ├── llm/externalcli/             # Headless agent CLI via DocGen contract v2
│   │   ├── embed/openai/                # OpenAI embeddings
│   │   ├── embed/bedrock/               # Bedrock Titan/Cohere embeddings
│   │   ├── embed/vertex/                # Vertex text embeddings
│   │   ├── embed/ollama/                # Local embeddings
│   │   ├── embed/openaicompat/          # Any OpenAI-compatible embeddings endpoint
│   │   ├── vector/pgvector/             # Default vector index
│   │   ├── vector/qdrant/               # Alternative vector index
│   │   ├── signal/cloudwatch/           # CloudWatch Logs + Alarms (poll + EventBridge)
│   │   ├── signal/gcp/                  # Cloud Logging, Error Reporting, Monitoring
│   │   ├── signal/datadog/              # Monitor webhooks + Events/Logs API
│   │   ├── signal/grafana/              # Grafana alerting webhooks
│   │   ├── signal/alertmanager/         # Prometheus Alertmanager webhooks
│   │   ├── signal/sentry/               # Sentry webhooks + Issues API
│   │   ├── signal/pagerduty/            # PagerDuty v3 webhooks + Incidents API
│   │   ├── signal/opsgenie/             # Opsgenie webhooks + Alerts API
│   │   ├── signal/wiz/                  # Wiz GraphQL issues/vulnerabilities
│   │   ├── signal/splunk/               # Splunk saved searches + alert webhooks
│   │   ├── signal/firehose/             # Amazon Data Firehose HTTP endpoint receiver
│   │   ├── signal/pubsub/               # GCP Pub/Sub pull consumer
│   │   ├── signal/generic/              # Any-tool JSON webhook with UI field mapping
│   │   ├── signal/eventbus/kafka/       # Kafka/MSK/Confluent lag + DLQ topics
│   │   ├── signal/eventbus/sqs/         # SQS DLQ depth/age + opt-in peek
│   │   ├── signal/eventbus/sns/         # SNS delivery failures + subscription DLQs
│   │   ├── signal/eventbus/eventbridge/ # Failed invocations + rule/target DLQs
│   │   ├── signal/eventbus/kinesis/     # Iterator age, throttling, consumer failure destinations
│   │   ├── signal/eventbus/pubsub/      # Backlog + dead-letter subscriptions
│   │   ├── signal/eventbus/rabbitmq/    # Queue depth, DLX queues, consumers
│   │   ├── knowledge/confluence/        # Confluence Cloud/Data Center sync + XHTML→MD
│   │   ├── knowledge/jira/              # Jira Cloud/Data Center read-only sync
│   │   ├── contextprovider/command/     # Operator command → stdout context
│   │   ├── contextprovider/http/        # Operator HTTP service → context
│   │   ├── secrets/awskms/              # AWS KMS key wrapping
│   │   ├── secrets/gcpkms/              # GCP KMS key wrapping
│   │   └── secrets/localfile/           # Local key file wrapping
│   ├── importer/                        # One-time Markdown/wiki import
│   └── bootstrap/                       # `dth up` orchestration
├── pkg/
│   └── contract/                        # DocGen v2, decode, answer schemas (importable)
├── migrations/                          # golang-migrate SQL files (NNNN_name.up/down.sql)
├── web/                                 # React + TS UI (Vite)
│   ├── src/
│   │   ├── api/                         # Generated OpenAPI client + TanStack Query hooks
│   │   ├── components/                  # Shared UI components (shadcn/ui)
│   │   ├── pages/                       # Ask, Inbox, IssueDetail, KnownIssues, Docs, Palace,
│   │   │                                #  Library, Repos, Connectors, Providers, Spend,
│   │   │                                #  Analytics, Activity, Users, Settings, Setup
│   │   ├── layouts/                     # App shell, nav, auth guard
│   │   └── lib/                         # Formatting, markdown, SSE client
│   ├── tests/                           # Vitest unit tests
│   ├── e2e/                             # Playwright specs
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── test/
│   ├── parity/                          # Port parity suites
│   ├── fixtures/                        # Repo bundles, signal payloads, LLM responses
│   ├── mocks/                           # HTTP mock servers per external API
│   ├── eval/                            # Golden Q&A and decode sets
│   └── perf/                            # k6 scripts
├── deploy/
│   ├── compose/docker-compose.yml       # Local stack
│   ├── helm/dth/                        # Helm chart (Chart.yaml, values.yaml, templates/)
│   ├── terraform/aws/                   # ECS Fargate, RDS, ALB, KMS, Secrets Manager, IAM
│   ├── terraform/gcp/                   # Cloud Run, Cloud SQL, KMS, Secret Manager, IAM
│   └── terraform/modules/readonly-roles/ # Least-privilege roles to create in watched accounts/projects
├── docker/Dockerfile                    # Multi-stage: web build → go build → distroless
├── grammars/                            # Optional runtime Tree-sitter grammars (empty)
├── docs/
│   ├── plan-doctherepo-hub.md           # This file
│   ├── plan-docrag-orchestrator.md      # Superseded original plan (kept for history)
│   ├── connectors/                      # One setup page per connector (permissions, webhook URL)
│   └── operations.md                    # Deploy, backup, upgrade, spend guidance
├── .github/workflows/ci.yml             # Lint, test, build, scan, e2e
├── sqlc.yaml
├── go.mod
├── go.sum
├── Makefile                             # build, test, lint, web, image, e2e targets
├── LICENSE                              # Existing MIT license (see § 15)
└── README.md
```

## 14. Implementation Order

Milestones are delivery checkpoints; phases inside them are build order. Every phase ends compiling, with
its tests passing.

### Milestone 1 — Repos → Docs → Ask, with UI, BYO LLM, analytics, local + cloud deploy
**Phase 1 — Foundation**: go.mod, Makefile, CI skeleton; config; migrations for identity, connectors,
repos, jobs, chunks, usage; store + sqlc; secrets (localfile); Postgres queue with per-repo ordering;
health endpoints; slog/metrics. Tests: queue concurrency, config validation.
**Phase 2 — Core code intelligence**: grammars, triage, chunker, manifest, docassembly, scopedcontext,
spendguard, palace extraction, library rules. Unit tests ≥ 85%.
**Phase 3 — LLM & embeddings**: ports + anthropic, openai, azureopenai, bedrock, vertex, openaicompat,
externalcli; embed adapters; model routing + fallback; usage recording; parity suites.
**Phase 4 — Git hosts & ingest**: GitHub + GitLab adapters, webhook ingress, polling ingest, push adapters
+ PR lifecycle; pipeline `code_push` end to end with pgvector; importer; reindex.
**Phase 5 — API + auth**: OIDC, sessions, PATs, RBAC, repo ACL; all M1 endpoints (§ 7.2–7.4, 7.6, 7.7, 7.8);
RAG engine + SSE; answer cache; OpenAPI spec.
**Phase 6 — Web UI (M1 pages)**: shell, Setup wizard, Ask, Docs Tree, Palace explorer, Library, Repos,
Connectors (git hosts), Providers & Routing, Spend, Activity & Analytics, Users; Vitest.
**Phase 7 — CLI & packaging**: `dth up/down/status/ask/repos/import/reindex/dry-run/retry/usage/token/
adapter-test/migrate`; Dockerfile; Compose; Terraform AWS + GCP; Helm; readonly-roles module.
**Phase 8 — M1 E2E & perf**: cold-start E2E, spend-block E2E, RBAC E2E, push-burst and Q&A k6 runs; eval set.

### Milestone 2 — Errors, alerts, known issues
**Phase 9 — Signal core**: migrations (events partitions, issues, decodes, known issues, savings);
normalization, redaction, fingerprinting, grouping, known-issue matcher; unit tests.
**Phase 10 — Signal adapters**: Firehose + Pub/Sub stream receivers with aggregation, Sentry, PagerDuty, Opsgenie, Datadog, Grafana, Alertmanager (webhooks),
CloudWatch (subscription → Firehose, alarms via EventBridge, poll for local), GCP (sink → Pub/Sub,
Monitoring webhook), generic webhook, event-bus inspectors (D14); fixture suite; parity suite.
**Phase 11 — Decode & suggestions**: decode pipeline, similar-issue search, re-decode triggers, known-issue
suggestions; savings accounting.
**Phase 12 — API + UI**: Inbox, Issue detail, Known Issues (with rule tester), Suggestions, connector
pages for signal sources, savings on Analytics; E2E error flow; signal-storm k6.

### Milestone 3 — Knowledge & security sources
**Phase 13 — Confluence & Jira**: sync, XHTML→Markdown, chunking, palace links, known-issue import from
labels, paste/link explain flow, Library shelves.
**Phase 14 — Wiz & Splunk**: adapters, fixtures, severity mapping, UI source filters.

### Milestone 4 — Hardening
**Phase 15**: govulncheck/npm audit/Trivy gates, SBOM, load results documented, operations guide,
per-connector setup docs, backup/restore runbook (pg_dump + KMS key custody), upgrade path.

## 15. Open Questions & Deferred Decisions

| Question | Assumption made to unblock | Revisit before |
|---|---|---|
| D4 Postgres as the single store (drops bbolt/NATS from the original plan) | Yes — enables multi-replica cloud deploy; at your volume it holds aggregates and samples, never raw logs | Phase 1 |
| Source-side filter cost | CloudWatch subscription filters + Firehose and GCP sinks + Pub/Sub are billed to your accounts; volume after ERROR filtering is expected to be a small fraction of total logs. Measure during Phase 10 with your real ratio | Phase 10 |
| D9 Scale | 30–40 repos; 20k error events/s burst ingest target; revisit if error-level volume after filtering exceeds that | Phase 12 perf run |
| Outbound notifications | Not in scope (non-goal); candidate: Slack/Teams daily digest of new decoded issues | After M2 |
| Opsgenie longevity | Atlassian has announced Opsgenie's end of sale and a future end of support (**verify current dates on Atlassian's site**); adapter still built, kept small | Phase 10 |
| Licence | Repo currently has **MIT**; the original plan said Apache-2.0. Assumed: keep MIT unless you choose otherwise | Phase 15 |
| Cost table accuracy | Seeded with list prices at build time; operator owns updates; UI shows "last verified" date | Phase 3 |
| GitHub Enterprise Server / GitLab Dedicated | Supported via `base_url`; untested until a test instance exists | Phase 4 |
| Data residency for LLM calls | Per-feature provider allow-list; no default restriction | Phase 3 |

Genuine unknowns (answered only by running it): whether one-hop context gives good docs (fallback:
ContextProvider); whether fingerprint rules over- or under-group for a given source (fallback: per-source
override to use the source's own group ID only, already built); Q&A quality on very large estates (the eval
set and feedback buttons measure it).

## 16. Agent Instructions

## Instructions for the Implementing Agent

1. **Do not start until the user has confirmed the Decisions table at the top of this plan.**
2. **Read the entire plan before writing code**, including all of § 8.
3. **Follow § 14 in order.** Each phase compiles and passes its tests before the next starts. Commit per
   phase with a descriptive message; push to the designated branch.
4. **Match § 13 exactly.** New files/directories require updating this plan first.
5. **Never import an adapter from `internal/core`.** The core talks to the world only through `internal/ports`
   and store repository interfaces.
6. **No paid call bypasses `core/spendguard`** — docgen, decode, Q&A, triage, suggestions, and embeddings
   alike. Use stub adapters in tests, never a bypass flag.
7. **No LLM call on data that a known-issue rule suppresses**, and **no unredacted signal text reaches an
   LLM** while redaction is enabled.
8. **Enforce repo ACL in SQL** for every read path that can surface repo content (retrieval, graph, docs,
   issues). Never filter after the LLM has seen content.
9. **Cloud and monitoring connectors are read-only.** Do not implement any write, acknowledge, silence, or
   remediation call.
10. **The approver flow never accepts a second user's credentials.**
11. **Parity suites are never weakened**; fix the port, not the test.
12. **Dependencies are limited to § 3.** Flag any addition before adding it.
13. **Apply § 9 everywhere**: typed errors, no swallowed errors, every failure visible in job status, API
    error contract, and logs.
14. **Log per § 11**; no `fmt.Println` in production paths; never log secrets or unredacted messages.
15. **No hardcoded secrets, URLs, or account IDs**; configuration comes from `internal/config`, the DB
    settings tables, or env.
16. **If you hit an item in § 15 that affects architecture, stop and ask** rather than deciding alone.

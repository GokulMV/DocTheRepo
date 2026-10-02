# DocTheRepo Hub — architecture (as built, through Phase 13)

This page describes what is implemented today, not the full plan (`docs/plan-doctherepo-hub.md`). Where
the code differs from the plan, it says so at the end.

## 1. Shape

One Go binary (`cmd/hub`) holds every role; a deployment runs it once (local) or as three services. It is a
**modular monolith with ports and adapters**: core logic depends only on interfaces in `internal/ports`;
everything that talks to the outside world is an adapter.

```
                    Browser (React UI, embedded)        dth CLI          Git hosts / signal tools
                               │ REST + SSE               │ REST           │ webhooks / Firehose / Pub/Sub
                               ▼                          ▼                ▼
┌──────────────────────────────────────── dth-hub (one binary) ──────────────────────────────────────────┐
│  role api        internal/api ── auth (OIDC, local owner, PATs, RBAC, repo ACL, audit) ── webui        │
│                  /api/v1/*  /hooks/{github|gitlab|firehose|<signal source>}/{id}  /readyz  openapi    │
│                                                                                                        │
│  role worker     queue.Pool ── job handlers:                                                           │
│                    code_push → pipeline.CodePush       import_docs → pipeline.ImportDocs               │
│                    reindex   → pipeline.Reindex        signal_batch → ingest.SignalPolls.Handle        │
│                    decode_issue → decode.Decoder       pr_review → lifecycle.Sweeper.OnReview          │
│                  + Pub/Sub stream consumers, signal aggregator                                         │
│                                                                                                        │
│  role scheduler  leader-elected (Postgres advisory lock): lease reclaim, partitions, git polling,     │
│                  signal-connector polling, PR lifecycle sweep, auto-suggestions, GC, spend reload     │
│                                                                                                        │
│  core/*  (pure logic)          llmgateway ── spendguard          ports/* (interfaces)                 │
│  adapters/* (codehost, llm, embed, push, signal, vector, secrets, knowledge)                           │
└──────────────────────────────┬─────────────────────────────────────────────┬───────────────────────────┘
                               ▼                                             ▼
                PostgreSQL 16 + pgvector (state, queue, chunks,     LLM / embedding providers
                vectors, graph, issues, usage) — or Qdrant for       (Anthropic, OpenAI, Azure, Bedrock,
                vectors                                               Vertex, Ollama, compat, Jev, CLI)
```

Roles are chosen with `--roles` / `DTH_ROLES` (`cmd/hub/main.go`); `cmd/hub/wire.go` is the composition
root that builds every adapter and service once.

## 2. Packages

| Layer | Packages | Responsibility |
|---|---|---|
| Ports | `internal/ports` | Interfaces and shared types: CodeHost, LLM, Embedder, DocGenerator, Decider, VectorIndex, SignalSink/Webhook/Poller, BusInspector, Lander, KnowledgeSource, … |
| Contracts | `pkg/contract` | Versioned JSON schemas for docgen, decode, and Q&A model output |
| Core | `core/chunker`, `grammars`, `triage`, `palace`, `library`, `docassembly`, `docgen`, `manifest`, `pipeline`, `scopedcontext` | Code intelligence and the docs pipeline (Tree-sitter chunks, AST triage, knowledge graph, shelves) |
| | `core/rag` | Q&A: hybrid retrieval, graph expansion, packing, citations, answer cache |
| | `core/llmgateway`, `core/spendguard` | The only path to paid models: routes, budgets, usage ledger, JSON validation + repair |
| | `core/signals`, `knownissues`, `aggregate`, `busrules`, `decide`, `decode`, `suggest` | Error intelligence: scrub/fingerprint, rule matcher, 1 s aggregation, bus rules, decision gate, decode, rule suggestions |
| Adapters | `adapters/codehost/{github,gitlab}`, `push/*` | Git reads, docs landing (direct, PR auto-merge, PR with approver) and PR lifecycle |
| | `adapters/llm/*`, `embed/*` | Model providers (incl. Jev for decisions) |
| | `adapters/signal/*` | Webhooks (Sentry, PagerDuty, Opsgenie, Datadog, Alertmanager/Grafana, AWS, GCP, generic), Firehose, Pub/Sub, CloudWatch/GCP pollers, Kafka/SQS/SNS/EventBridge/Kinesis/Pub/Sub/RabbitMQ inspectors |
| | `adapters/vector/{pgvector,qdrant}`, `secrets/{localfile,awskms,gcpkms}` | Vector index, envelope-encryption keys |
| | `adapters/knowledge/{confluence,jira}` | Read-only Confluence (CQL) and Jira (JQL) sync, storage-XHTML/ADF → Markdown |
| App | `ingest`, `api`, `auth`, `queue`, `scheduler`, `store`, `config`, `observability`, `bootstrap`, `webui` | Job/ingress orchestration, HTTP, identity, Postgres queue, leader tasks, sqlc store, config, logs/metrics, `dth up`, embedded UI |

## 3. Flows

**A. Code push → docs → index**
`/hooks/github|gitlab` (or polling) → `code_push` job → `pipeline.CodePush`: load diff → AST triage (LLM
only for ambiguous files) → chunk → docgen (route `docgen`) → assemble doc files → land via push mode →
persist: chunk manifest, embeddings (route `embedding`), Palace graph, docs Tree, Library shelves, savings.

**B. Signals → Inbox**
Webhooks / Firehose / Pub/Sub / pollers / bus inspectors → `SignalIngest.Ingest`: scrub + PII + normalize
+ fingerprint → known-issue matcher (suppress / label / accept) → 1 s aggregator → `issues`, samples, counts
→ new issue enqueues `decode_issue` → decode: reuse if code unchanged → decision gate (Jev or chat JSON;
skips only confident "known noise") → full decode (12k-token context: samples, frame→code, commits, similar
issues, runbooks) → indexed as `issue_decode`. Daily auto-suggestions and paste-text proposals create rules
that a human enables.

**D. Confluence & Jira**
Scheduler (`sync_knowledge_connectors`, every 30 s, per-connector interval 15 min) → `knowledge_sync` job →
`ingest.KnowledgeSync`: per space/project, changed documents since the cursor → `store.Knowledge.Apply`
(chunks without a repo — readable by every viewer — via the manifest diff, `knowledge_docs`, Palace entity
plus `documented_in`/`runbook_for` links to mentioned services, repos, endpoints, Library shelves) → embed →
known-issue upstream check (Jira Done flips a suppressing rule to label only) → cursor stored. Daily
reconcile removes deleted pages; the `known-issue` label query creates disabled draft rules (match proposed
by the suggest route). `POST /known-issues/from-link` fetches a URL through the owning connector.

**C. Q&A**
`POST /api/v1/ask` (SSE) → answer cache (question + scope + index version) → embed + vector search and
full-text search → reciprocal rank fusion → ACL-scoped chunks → one-hop graph expansion → budget packing →
model (route `qa`) → citation check → cache.

## 4. Data

PostgreSQL 16 + pgvector, migrations `0001`–`0014`: identity and audit; connectors, cursors, repos, ACLs,
providers, routes, prices; the job queue; chunks (+ tsvector) and index version; month-partitioned usage
and savings; Tree / Palace / Library / PRs; Q&A threads and cache; known issues, issues, decodes, samples,
minute/hour counts, suggestions; decision-gate and Jev columns; synced Confluence/Jira documents (`knowledge_docs`) and upstream state on imported rules. Vectors live in pgvector (default) or
Qdrant. Secrets (connector credentials, provider keys) are AES-GCM envelope-encrypted with a local, AWS KMS,
or GCP KMS key.

## 5. Interfaces

- **REST** `/api/v1` (OpenAPI at `/api/v1/openapi.json`): Auth, Users, Ask, Docs, Palace, Library, Repos,
  Connectors, Providers, Spend, Jobs, Analytics, Inbox, Known issues; ingress under `/hooks`.
- **Web UI** (`web/`, React + TypeScript): Setup, Ask, Docs, Palace, Library, Inbox, Issue, Known Issues,
  Repos, Connectors, Providers & routing, Spend, Analytics, Activity, Users, Account.
- **CLI** `dth`: up/down/status, login, ask, repos (add, import, dry-run), jobs/retry, usage, tokens,
  reindex, adapter-test, migrate; `dth engine opencode` (opencode as a doc engine via `external_cli`) and
  `dth mcp` (the Hub as a read-only MCP server for opencode, Claude Code, Cursor) — see `docs/opencode.md`.

## 6. Delivery and tests

- `scripts/quickstart.sh` — one command: prerequisites, build, Postgres, hub, sign-in, UI.
- `deploy/compose`, `deploy/helm/dth`, `deploy/terraform/{aws,gcp}` (+ read-only cloud roles).
- Tests: unit + testcontainers integration (`make test`), adapter parity suites (`test/parity`), Playwright
  E2E against the real hub with mocks (`test/e2e`), k6 load (`test/perf`: push burst, Q&A, signal storm),
  eval sets (`test/eval`: golden Q&A, decision calibration).

## 7. Not built yet / differs from the plan

- Wiz and Splunk adapters (rest of Milestone 3) and Milestone 4 hardening.
- Docgen's scoped context does not yet include linked Confluence sections (the decode context does use
  Confluence runbooks).
- Beyond the plan's § 4 and built: the decision gate (Jev, Phase 11.5) and event-bus inspectors.

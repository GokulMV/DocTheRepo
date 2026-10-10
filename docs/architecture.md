# DocTheRepo Hub — architecture (as built, through Phase 14)

This page describes what is implemented today.

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

### Diagrams

Interactive diagrams (standalone HTML, open offline in a browser; generated with
[archify](https://github.com/tt-a1i/archify) from the JSON next to each file, with source links pinned to
commit `d2b1330`):

| Diagram | Type | Files |
|---|---|---|
| System architecture: roles, ports & adapters, stores, providers, clients | architecture | [HTML](architecture/system-architecture.html) · [JSON](architecture/system-architecture.architecture.json) |
| Flow A: code push → docs → index | dataflow | [HTML](architecture/code-push-docs.html) · [JSON](architecture/code-push-docs.dataflow.json) |
| Flow B: signals → Inbox → decode (incl. never-send-to-LLM) | workflow | [HTML](architecture/signals-inbox.html) · [JSON](architecture/signals-inbox.workflow.json) |
| Flow C: Q&A (RAG) request | sequence | [HTML](architecture/qa-rag.html) · [JSON](architecture/qa-rag.sequence.json) |
| Flow D: Confluence & Jira knowledge sync | dataflow | [HTML](architecture/knowledge-sync.html) · [JSON](architecture/knowledge-sync.dataflow.json) |

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
| | `adapters/signal/*` | Webhooks (Sentry, PagerDuty, Opsgenie, Datadog, Alertmanager/Grafana, AWS, GCP, Wiz, Splunk, generic), Firehose, Pub/Sub, CloudWatch/GCP/Wiz/Splunk pollers, Kafka/SQS/SNS/EventBridge/Kinesis/Pub/Sub/RabbitMQ inspectors |
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
+ fingerprint (events from a never-send-to-LLM connector are marked `dth.no_llm`) → known-issue matcher (suppress / label / accept) → 1 s aggregator → `issues`, samples, counts
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
`POST /api/v1/ask` (SSE) → answer cache → embed + vector search and full-text search (plus READMEs and
overview docs for broad questions) → reciprocal rank fusion → ACL-scoped chunks → one-hop graph expansion →
budget packing → model (route `qa`) → citation check → cache.

Ask is retrieval-augmented generation with an agent fallback:

- **Usually:** one retrieval pass and one model call.
- **When retrieval finds too little:** fewer than 3 sources fit, or the first answer cites nothing. The
  model then investigates for up to `ask.agent_steps` steps (default 4; `DTH_ASK_AGENT_STEPS`, 0 turns it
  off). In each step it chooses to search again with other words, read a whole file, list files, or
  answer. Then it answers from everything gathered, through the same citation contract.
- **What it can see:** every step reads through the same ACL-scoped store. Steps are plain JSON
  decisions (structured output), so the agent works with any chat provider, not only those with tool use.
- **While it works:** the UI shows each step ("Searched for …", "Read …"). The first round's "not found"
  is held back, and an uncited answer that was already streamed is withdrawn (an SSE `reset` event).
- **Cost:** at most 1 + `agent_steps` + 1 model calls, and only for questions that need it. The result is
  cached like any other answer, so asking again costs nothing.

Before answering, the **source picker** ([ask-sources.md](ask-sources.md)) has a cheap judge (TypeSafe Jev or a
small chat model) answer yes/no relevance and scope questions for every retrieved piece, many per call, and
sends the answering model only the pieces that pass. When search finds too little, it explores the indexed
tree directory by directory before the agent spends model calls.

Two caches keep repeated questions cheap:

- **Answer cache** (Postgres, `answer_cache`). The first question of a thread is looked up by its
  fingerprint plus the asker's scope. The fingerprint is the question lower-cased, without punctuation,
  filler words ("please", "can you explain", "the") or plural endings, so rephrasings share an entry. A hit
  returns the stored answer with no model call. An entry stays valid for up to 7 days, until a chunk it
  cites is removed or anything in a repository or space it cites changes. Pushes to other repositories
  don't expire it. Only answers with citations are cached. Follow-up questions in a thread depend on the
  conversation, so they always go to the model.
  When the wording differs more than that, the question's embedding (computed for retrieval anyway, so
  no extra call) is compared with the cached answers' questions from the same scope and embedding model.
  At a cosine similarity of 0.95 or more (`ask.similar_answer`, `DTH_ASK_SIMILAR_ANSWER`; 0 turns it off)
  the stored answer is reused, with the same freshness rules. A question that names a file or identifier
  (`refund.go`, `PayRetry`, `retry_payment`) only reuses an answer to a question naming the same ones.
- **Prompt cache** (Claude on the Claude API, Bedrock and Vertex). Each request marks the system prompt and
  the last message as cache breakpoints. The next turn of a thread, or the next file of a docs job, then
  reads the repeated prefix at about a tenth of the input price. Prefixes below the model's minimum length
  are not cached. OpenAI and Gemini cache repeated prefixes on their own. Cache reads and writes are
  reported per call; spend limits count them as full-price input, which errs on the safe side.
- **Long-prompt prices.** A cost-table row can carry a long-prompt tier (`long_prompt_threshold_tokens`
  and the `long_*_per_mtok_usd` prices). When a prompt (input plus cache reads and writes) is over the
  threshold, the whole request, output and cache included, is priced at the long rates; the spend guard's
  pre-call estimate uses the same rule. A long cache price left empty is the long input price. Claude
  Haiku 5.5 is seeded with a 100K-token threshold at 5x its base prices. Rows without a tier price every
  prompt the same.

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
- **Web UI** (`web/`, React + TypeScript): Ask; Docs (Docs, Architecture, Team docs); Issues (Inbox,
  Known issues; shown once an alert tool is connected); Repositories (Repositories, Activity, Security); Usage;
  Settings (Connections, AI models, People, Advanced); Account.
- **CLI** `dth`: up/down/status, login, ask, repos (add, import, dry-run), jobs/retry, usage, tokens,
  reindex, adapter-test, migrate; `dth engine opencode` (opencode as a doc engine via `external_cli`) and
  `dth mcp` (the Hub as a read-only MCP server for opencode, Claude Code, Cursor) — see `docs/opencode.md`.

## 6. Delivery and tests

- `scripts/quickstart.sh` — one command: prerequisites, build, Postgres, hub, sign-in, UI.
- `deploy/compose`, `deploy/helm/dth`, `deploy/terraform/{aws,gcp}` (+ read-only cloud roles).
- Tests: unit + testcontainers integration (`make test`), adapter parity suites (`test/parity`), Playwright
  E2E against the real hub with mocks (`test/e2e`), k6 load (`test/perf`: push burst, Q&A, signal storm),
  eval sets (`test/eval`: golden Q&A, decision calibration).

## 7. Not built yet

- Milestone 4 hardening (security gates, SBOM, operations and backup runbooks).
- Docgen's scoped context does not yet include linked Confluence sections (the decode context does use
  Confluence runbooks).
- Beyond the plan's § 4 and built: the decision gate (Jev, Phase 11.5) and event-bus inspectors.

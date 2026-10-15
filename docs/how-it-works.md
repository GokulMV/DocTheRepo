# How DocTheRepo Hub works, end to end

A plain-language tour of the whole system: what runs, how code becomes documents, how Ask answers, how
live lookups (MCP) and sign-ins work, how alerts become issues, and how cost and secrets are kept under
control. For package-level detail see [architecture.md](architecture.md); each section below links to
the page that goes deeper.

---

## 1. The big picture

```
   People                          Your systems                          AI providers
   ──────                          ────────────                          ────────────
   Browser (Hub UI)                GitHub / GitLab (code, webhooks)      Anthropic, OpenAI, Azure,
   dth CLI                         Sentry, Datadog, PagerDuty… (alerts)  Bedrock, Vertex, Ollama…
   Claude Code / Cursor (MCP)      Confluence, Jira (knowledge)
                                   AWS, Google Cloud (logs, queues)
        │                                   │                                  ▲
        ▼                                   ▼                                  │
┌───────────────────────────────── dth-hub (one Go binary) ─────────────────────┼──────────────┐
│                                                                               │              │
│  API role ─────── REST + streaming answers, web UI, webhooks, sign-in, roles  │              │
│     │                                                                         │              │
│     ▼ jobs (Postgres queue)                                                   │              │
│  Worker role ──── code_push · repo_docs · system_docs · knowledge_sync ·      │              │
│                   signal_batch · decode_issue · pr_review · security_scan     │              │
│     │                                                                         │              │
│  Scheduler role ─ polling, retries, clean-up, auto-suggestions (one leader)   │              │
│                                                                               │              │
│  LLM gateway ──── the ONLY way to call a model: picks the route, checks the ──┘              │
│                   spend limit first, scrubs secrets, records usage                           │
└──────────────────────────────────────┬───────────────────────────────────────────────────────┘
                                       ▼
          PostgreSQL 16 + pgvector: settings, users, jobs, code chunks, embeddings,
          knowledge graph, documents, issues, usage ledger, sealed secrets
```

- **One binary, three roles.** Locally everything runs in one process. In the cloud it is usually split
  into API, worker and scheduler services from the same image (`DTH_ROLES`).
- **Ports and adapters.** Core logic (`internal/core`) only talks to interfaces (`internal/ports`).
  Everything that touches the outside world — GitHub, model providers, AWS, Atlassian — is an adapter
  (`internal/adapters`). Swapping a provider never touches the core.
- **Postgres is the only required service.** It also serves as the job queue and the vector index
  (pgvector; Qdrant is optional).

---

## 2. Connecting a repository

1. **Connect with GitHub** creates and installs the Hub's own GitHub App on GitHub's page (you pick the
   repositories there). GitLab uses a token. Existing GitHub Apps can be added by App ID and key.
2. **Add a repository.** The Hub reads it and queues the first documentation run.
3. **Every push** arrives as a webhook (or by polling) and becomes a `code_push` job.

Deeper: [github.md](github.md), [connectors.md](connectors.md).

---

## 3. From code to documents

```
push ─▶ code_push job ─────────────────────────────────────────────▶ repo_docs job ─▶ system_docs job
        │ read the diff                                               │ write the documents   │ link repos
        │ parse with Tree-sitter (functions, types, imports)          │ check every claim     │ together
        │ update the knowledge graph (calls, imports, endpoints)      │ score confidence
        │ split into chunks, embed them for search                    │ index for Ask
        └ triage: which files changed meaningfully                    └ export to git on request
```

**Step 1 — understand the code (`code_push`, no model needed for most of it).**
- Each changed file is parsed into functions, types and imports.
- The **knowledge graph** records what calls what, which package imports which, which endpoints exist.
  Go imports of the repository's own packages become "used by" links between modules.
- Code is split into chunks and embedded, so Ask can search it by meaning and by keyword.

**Step 2 — write the documents (`repo_docs`).**
- The repository is split into **modules** (folders that belong together).
- The Hub writes an **Overview**, an **Architecture** document, a **guide per module**, and the other
  documents that apply (Flows, API reference, Data model, Configuration, Test architecture, Runbooks,
  Security, Glossary, Recent changes, Decision records…). A document is only written when the repository
  has what it describes.
- Each document gets a focused **material pack**: the relevant code, the graph facts, config files, test
  names with line numbers, recent commits — never the whole repository.
- **Every claim cites the code** as `[path:line]`. A checker verifies the citations, the names and the
  section structure. If the first draft has problems, the model gets **one repair pass** with the list of
  problems.
- Each document and section gets a **confidence score** so readers know how far to trust it.
- Documents only get rewritten when what they describe changes (content hashes), so most pushes cost
  little or nothing.

**Step 3 — link repositories (`system_docs`).**
- The **System** page connects repositories: one calls another's API, deploys another's container image,
  or triggers its pipeline (found in config files and CI definitions).
- It is searchable in Ask only by people who can read every repository it was written from.

**Where documents live:** in the Hub under **Docs**. **Export to repository** opens a pull request with
them as Markdown whenever you want them in git.

Deeper: [docs-generation.md](docs-generation.md).

---

## 4. Asking questions (Ask)

```
question ─▶ answer cache? ─yes─▶ stored answer (no model call)
               │ no
               ▼
           search: meaning (embeddings) + keywords + overview docs
               ▼
           only what this person may read (repository access rules)
               ▼
           source picker: a cheap judge keeps only relevant pieces
               ▼
           model answers with citations ─▶ citations checked ─▶ cached
               │
               └─ too little found? agent mode: search again, read files,
                  call live tools (MCP), up to a few steps, then answer
```

- **Mostly one model call.** Agent mode only kicks in when search finds too little or the question is
  about live state.
- **Caching:** the same or a very similar question reuses the stored answer until the code it cited
  changes. Repeated prompt prefixes are prompt-cached at about a tenth of the price.
- **Access rules apply everywhere:** a person only ever gets answers built from repositories and spaces
  they can read.

---

## 5. Live lookups in other products (MCP connections)

Ask can query other products **live** while answering — Sentry errors right now, Datadog logs, AWS
resources, GitHub issues, Jira tickets — through each product's MCP server.

```
Ask agent ─▶ picks an allowed tool ─▶ Hub's MCP client ─▶ product's MCP server
                                         │ attaches the connection's sign-in
                                         ▼
                                  result comes back as a "live" source in the answer
                                  (never stored, never cached)
```

**How a connection signs in** (in order of preference):

| Method | Used by | What the person does |
|---|---|---|
| **Sign in on the product's own page (OAuth)** | AWS, Sentry, Datadog, Grafana, PagerDuty, Honeycomb, Cloudflare, Atlassian, Notion, Linear, Slack, GitLab, Supabase, New Relic, Stripe, Azure DevOps | Click **Connect**, approve on the product's page, come back connected. The Hub registers itself automatically and renews the sign-in. |
| **Sign in with GitHub (the Hub's GitHub App)** | GitHub | Click **Sign in with GitHub**, approve on GitHub. |
| **Cloud identity** | AWS, Google Cloud | Use the Hub's own IAM role or service account, or a **read-only role** created with one click in the AWS console. Best for always-on use. |
| **Token or key** | Splunk Enterprise, Elastic, Dynatrace, and as a fallback everywhere | Paste a read-only key; it is sealed in the browser. |

**Safety:** only tools marked read-only are on by default; each connection sets the lowest role that may
use it; tool results are treated as data, never as instructions; a lapsed sign-in shows **Needs sign-in
again** instead of failing silently.

**The other direction — the Hub as an MCP server.** `dth mcp` lets Claude Code, Cursor or opencode use the
Hub as a tool: `ask`, `list_docs`, `read_document`, `search_entities`, `entity_graph`, `list_issues`,
`get_issue`, `list_known_issues`, `library` (all read-only).

Deeper: [mcp-connections.md](mcp-connections.md).

---

## 6. Knowledge from Confluence and Jira

- **Connect with Atlassian** (approve on Atlassian's page; the admin registers the Hub with Atlassian
  once) or an API token / Data Center token.
- A `knowledge_sync` job runs every 15 minutes per connector: changed pages and issues → Markdown →
  chunks → embeddings → graph links to the services and repos they mention.
- Pages labelled `known-issue` become draft known-issue rules for the Inbox.

Deeper: [confluence-jira.md](confluence-jira.md).

---

## 7. Production signals → Inbox → decode

```
alerts (webhooks, Firehose, Pub/Sub, pollers: CloudWatch, GCP Logging, Wiz, Splunk; queue inspectors)
   ▼
scrub secrets and personal data, normalise, fingerprint
   ▼
known-issue rules: suppress / label / let through
   ▼
group per second into issues (Inbox)
   ▼ new issue
decode_issue: cheap gate ("is this known noise?") ─▶ full decode with the code, commits, runbooks,
                                                      similar issues ─▶ explanation + suggested fix
```

- Connectors marked **never send to an AI model** keep their events out of every model call.
- **AWS sources** (CloudWatch, SQS, SNS, EventBridge, Kinesis) can use a **read-only role created with one
  click in the AWS console**: the role trusts only the Hub, with a secret External ID.

Deeper: [connectors.md](connectors.md).

---

## 8. Cost control

Every model call goes through the **LLM gateway**, which:

1. Picks the **route** for the feature (docs, Ask, decode…) — each route names a provider and model. An
   Anthropic route with no model uses Claude Haiku 5.5.
2. **Reserves the call's worst-case cost** before calling, against every spend limit it counts against and
   the repository's monthly docs budget. A **5% safety buffer** stays free under every dollar cap, so caps
   are not exceeded even with many documents written in parallel (`DTH_SPEND_BUFFER_PCT`).
3. **Scrubs secrets** from what it sends.
4. **Records actual usage** (input, output, cache reads and writes) at the model's real prices, including
   long-prompt tiers (for example Haiku 5.5 above 100K tokens).

A blocked call ends the job as `spend_blocked`; documents already being written finish and are kept.

---

## 9. Security in one paragraph each

- **Sign-in to the Hub:** your identity provider (OIDC/SSO) or local passwords; personal access tokens for
  the CLI; roles viewer / editor / admin / owner plus per-repository access; every change is audit-logged.
- **Secrets:** every key, token and sign-in (provider keys, GitHub App keys, OAuth refresh tokens,
  Atlassian app secret) is **sealed in the browser** and stored encrypted under the Hub's master key
  (local file, AWS KMS or GCP KMS). Nobody can read one back; `dth-hub rotate-key` re-encrypts them all.
- **Least privilege:** live lookups default to read-only tools; the AWS role defaults to the six read
  actions the Hub uses; GitHub sign-in is limited to repositories the App is installed on.

---

## 10. Running it

| Where | How |
|---|---|
| Your laptop | `git pull origin main && ./scripts/quickstart.sh` (Postgres in Docker, Hub native). Forgot the password: `./scripts/quickstart.sh --reset-password`. |
| Docker Compose | `deploy/compose` |
| Kubernetes | Helm chart in `deploy/helm` |
| AWS / Google Cloud | Terraform modules in `deploy/terraform` |

CI (GitHub Actions) runs Go tests with a real Postgres, web tests, end-to-end browser tests, a container
image scan, an SBOM and govulncheck on every push to `main`. A manual `docs-real-run` workflow writes this
repository's documents with a real model under a dollar cap and prints a quality report.

<p align="center">
  <img src="docs/brand/dth-symbol-color.svg" alt="" width="72" height="72">
</p>

<h1 align="center">DocTheRepo Hub</h1>

<p align="center">
  Living documentation, answers and error intelligence for every repository: self-hosted, with your own model keys.
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
  <img alt="Go 1.25" src="https://img.shields.io/badge/go-1.25-00ADD8.svg">
  <img alt="PostgreSQL 16 + pgvector" src="https://img.shields.io/badge/postgres-16%20%2B%20pgvector-336791.svg">
</p>

---

DocTheRepo Hub watches your repositories. On every push it writes and updates documentation where the code
changed. It answers questions about your systems with citations, maps how services, endpoints, topics and
datastores connect, and explains the errors and alerts your tools report.

It runs in your infrastructure: one Go binary, PostgreSQL, and the LLM provider accounts you already have
(Claude, OpenAI, Azure OpenAI, Bedrock, Vertex, Ollama or any OpenAI-compatible server). Hard spend limits
are checked before every paid call.

<p align="center">
  <img src="docs/images/architecture.png" alt="The Architecture tab: a repository's callers, endpoints, service, topics, datastores and dependencies" width="860">
</p>

## Features

| | |
|---|---|
| **Docs that keep up** | Each push is triaged by syntax tree: cosmetic changes cost nothing, and structural ones regenerate only the affected sections. Docs land directly, as an auto-merged PR, or as a PR for an approver. Hand-written blocks survive regeneration. |
| **Ask** | Hybrid retrieval (vectors + full text + the knowledge graph) over code, docs, Confluence and Jira, scoped to what the person may read. Answers stream with citations, and history is kept per user. |
| **Palace & Architecture** | A knowledge graph extracted from code: repositories, services, endpoints, topics, datastores, docs. Every repository gets a generated architecture diagram; diagrams authored with [archify](https://github.com/tt-a1i/archify) appear next to it. |
| **Inbox** | Sentry, Datadog, PagerDuty, Opsgenie, Grafana, Alertmanager, CloudWatch, GCP, Wiz, Splunk, Kafka/SQS/Pub/Sub/RabbitMQ lag and DLQs. Signals are scrubbed, grouped, matched against known issues and explained with the code behind them. Any source can be marked never-send-to-a-model. |
| **Connect in one click** | **Connect with GitHub** creates a private GitHub App for the Hub and installs it on the repositories you pick, with nothing to copy. GitLab and tokens work too. |
| **Sealed secrets** | Provider keys and credentials are sealed in the browser to the Hub's hybrid X25519 + ML-KEM-768 key, stored with AES-256-GCM under a local or KMS key, and are write-only. |
| **Settings as code** | Configure providers, routing, connectors, repositories and limits from YAML/JSON, with secrets as references to env, files, Vault, gopass, AWS or GCP secret managers (`dth apply -f`). |
| **Built to run** | Roles (api, worker, scheduler) scale independently. Ships with a Postgres job queue, Prometheus metrics, OIDC, RBAC and per-repository access, an audit log, Helm, Terraform for AWS/GCP, CycloneDX SBOMs, and vulnerability gates in CI. |

<p align="center">
  <img src="docs/images/ask.png" alt="Ask, with question history" width="49%">
  <img src="docs/images/palace.png" alt="The Palace knowledge graph" width="49%">
</p>

## Quick start

One command checks and installs what is missing (Go, Node.js, Docker), builds the Hub, starts PostgreSQL,
creates the owner account and opens the UI:

```sh
git clone https://github.com/GokulMV/DocTheRepo && cd DocTheRepo
./scripts/quickstart.sh
```

To get the latest version later:

```sh
cd DocTheRepo && git pull && ./scripts/quickstart.sh
```

Optionally set `ANTHROPIC_API_KEY` and/or `OPENAI_API_KEY` first to configure models automatically. The
sign-in details are printed and saved in `~/.dth-quickstart/credentials.txt`. Stop with
`./scripts/quickstart.sh --down`.

Re-running the quickstart (for example after `git pull`) keeps everything: connectors, the GitHub App,
provider keys, routing, repositories and docs live in the database and `~/.dth-quickstart` (which holds the
master key that encrypts your keys; back it up). It never overwrites routing you changed in the UI. Only
`--down --wipe` deletes data.

Then, in the UI:
1. **Connectors → Connect with GitHub**, and pick repositories.
2. **Providers & routing:** add your model provider.
3. **Ask** away.

More ways to run it, from Docker Compose with `dth up` to Kubernetes (Helm) and AWS/GCP (Terraform), are in
[docs/install.md](docs/install.md).

## Documentation

| Topic | |
|---|---|
| Install & deploy | [docs/install.md](docs/install.md) |
| How docs are generated, and `.dthignore` | [docs/docs-generation.md](docs/docs-generation.md) |
| Architecture | [docs/architecture.md](docs/architecture.md) · [diagrams](docs/architecture/) |
| Connecting GitHub | [docs/github.md](docs/github.md) |
| Settings file (YAML/JSON, secret references) | [docs/settings-file.md](docs/settings-file.md) |
| Architecture tab | [docs/architecture-tab.md](docs/architecture-tab.md) |
| Confluence & Jira · Wiz & Splunk · Jev | [confluence-jira](docs/confluence-jira.md) · [wiz-splunk](docs/wiz-splunk.md) · [jev](docs/jev.md) |
| opencode, Claude Code, Cursor (MCP) | [docs/opencode.md](docs/opencode.md) |
| Security model & sealed secrets | [docs/security.md](docs/security.md) |
| Performance results | [docs/perf-results.md](docs/perf-results.md) |
| What is built, phase by phase | [docs/status.md](docs/status.md) |
| Development | [docs/development.md](docs/development.md) |
| Brand & logo | [docs/brand/](docs/brand/) |

## Contributing

Bug reports, ideas and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md). Please follow the
[Code of Conduct](CODE_OF_CONDUCT.md), and report security issues privately as described in
[SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 2026 GokulMV. Third-party components and their licenses are listed in
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).

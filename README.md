# DocTheRepo Hub

A self-hosted engineering intelligence hub: it connects your repositories, cloud consoles (AWS, GCP),
monitoring and alerting tools, event platforms, security tools, and knowledge bases; keeps generated
documentation and a searchable index in sync with every push; answers questions with citations; and decodes
errors and alerts into one inbox, while a known-issues registry keeps LLM spend off problems you already
understand. You bring your own LLM provider and keys.

The full design is in [`docs/plan-doctherepo-hub.md`](docs/plan-doctherepo-hub.md).

## Status

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

## Develop

Requirements: Go (version in `go.mod`), Docker (integration tests start PostgreSQL + pgvector with
testcontainers), and `sqlc` if you change SQL.

```sh
make test        # all tests; integration tests require Docker
make test-unit   # tests that need no Docker (integration tests are skipped)
make build       # ./bin/dth-hub
```

Run the hub against a local database:

```sh
docker run -d --name dth-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=dth -p 5432:5432 pgvector/pgvector:pg16
export DTH_DATABASE_URL='postgres://postgres:pw@localhost:5432/dth?sslmode=disable'
./bin/dth-hub            # all roles; no config file needed
curl localhost:8080/readyz
```

## License

MIT — see [LICENSE](LICENSE).

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

## Install

### Local (one command)

Requires Docker. `dth up` writes `~/.dth/compose.yaml`, starts PostgreSQL (pgvector) and the hub, creates
the owner account (the password is printed once), and signs the CLI in. Re-running it is the health check;
`dth up --upgrade` pulls newer images.

```sh
make build                      # or download a dth release binary
./bin/dth up --owner-email you@acme.com
./bin/dth status
./bin/dth ask "how are refunds retried?" --repo acme/payments
./bin/dth down                  # --wipe also deletes the database and keys
```

Add `--ollama` to also run Ollama for local models. The UI is at <http://localhost:8080>.

### AWS — `deploy/terraform/aws`

ALB (ACM TLS) → ECS Fargate services `api` (2+), `worker` (1+), `scheduler` (exactly 1) → RDS PostgreSQL 16
(Multi-AZ, encrypted, TLS enforced). Secrets live in Secrets Manager; connector credentials and LLM keys
are envelope-encrypted with a customer-managed KMS key. Bring your own VPC (private subnets with NAT).

```sh
cd deploy/terraform/aws
cp terraform.tfvars.example terraform.tfvars    # VPC, subnets, domain, certificate, image, IdP
export TF_VAR_oidc_client_secret=...           # never commit it
terraform init && terraform apply
```

Register the `oidc_redirect_url` output with Okta/Google. `oidc_allowed_domains` is required: without it
any account at the IdP could sign in, and the first sign-in becomes owner.

### GCP — `deploy/terraform/gcp`

HTTPS load balancer (Google-managed certificate) → Cloud Run `api`; Cloud Run `worker` and `scheduler`
with CPU always allocated; Cloud SQL PostgreSQL 16 on a private IP (regional HA); Secret Manager; Cloud KMS.
Cloud Run cannot pull from ghcr.io, so mirror the image to Artifact Registry (or use a remote repository).

```sh
cd deploy/terraform/gcp
cp terraform.tfvars.example terraform.tfvars
export TF_VAR_oidc_client_secret=...
terraform init && terraform apply
# then point an A record for domain_name at the load_balancer_ip output
```

### Kubernetes — `deploy/helm/dth`

Deployments for the three roles (read-only root filesystem, non-root, dropped capabilities), a
PodDisruptionBudget, optional Ingress and ServiceMonitor. PostgreSQL 16 with pgvector is external. Use
IRSA / GKE Workload Identity with `secrets.provider=awskms|gcpkms`, or `localfile` with a shared key Secret.

```sh
kubectl create secret generic dth-db --from-literal=url='postgres://…?sslmode=require'
kubectl create secret generic dth-oidc --from-literal=client-secret=…
helm install dth deploy/helm/dth \
  --set publicURL=https://docs-hub.acme.com \
  --set database.existingSecret=dth-db \
  --set secrets.provider=awskms --set secrets.kmsKeyId=arn:aws:kms:… \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::…:role/dth \
  --set auth.oidc.issuer=https://acme.okta.com --set auth.oidc.clientId=… \
  --set auth.oidc.existingSecret=dth-oidc --set 'auth.oidc.allowedDomains={acme.com}' \
  --set ingress.enabled=true --set ingress.className=nginx
```

### Watched accounts — `deploy/terraform/modules/readonly-roles`

Apply `aws/` in each AWS account and `gcp/` in each GCP project the Hub should read from. Everything
granted is read-only (logs, alarms, metrics, queue/topic/stream metadata); SQS message peeking is opt-in,
and Pub/Sub dead-letter sampling uses Hub-owned subscriptions only.

## Develop

Requirements: Go (version in `go.mod`), Docker (integration tests start PostgreSQL + pgvector with
testcontainers), and `sqlc` if you change SQL.

```sh
make test        # all tests; integration tests require Docker
make test-unit   # tests that need no Docker (integration tests are skipped)
make build       # ./bin/dth-hub (API only unless the UI was staged) and ./bin/dth
make image       # container image with the UI embedded (docker/Dockerfile)
make deploy-lint # terraform fmt/validate + helm lint
make e2e         # Playwright E2E (real hub + Postgres + GitHub/OIDC mocks + stub model)
make stack       # that stack on its own, for manual testing (URLs printed as JSON)
make perf-push   # k6 push burst; make perf-qa for the 250k-chunk Q&A run (needs k6)
make web         # build the React UI and stage it for embedding (then `make build`)
make web-test    # UI typecheck + unit tests
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

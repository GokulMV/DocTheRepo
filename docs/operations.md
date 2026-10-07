# Operations

How to run the Hub day to day: what to monitor, how to back up and restore, how to upgrade, and what to do
when something goes wrong. To deploy it, see [install.md](install.md) and the [README](../README.md#deploying-for-your-team).

## What runs

| Role | What it does | Replicas |
|---|---|---|
| `api` | Web UI, REST API, webhooks, Ask; applies the settings file on start | 2+ behind a load balancer |
| `worker` | Docs generation, indexing, imports, error explanations, knowledge sync | 1+; scale with queue depth |
| `scheduler` | Polling git hosts and signal sources, PR sweeps, retention and cleanup | 1 (extra ones wait as standbys) |

All roles are the same image (`DTH_ROLES=api,worker,scheduler`, comma-separated). A single process with all
three is fine for small teams. All state is in PostgreSQL 16 with pgvector, including the job queue. Nothing
needs a shared disk except `localfile` master keys (below).

**Sizing to start with:**
- **Each role:** 1 vCPU and 1 GB RAM.
- **Workers doing docs generation** spend most of their time waiting on the model provider. Raise
  `queue.concurrency.code_push` (default 4) and `queue.concurrency.repo_docs` (default 2) before adding CPU.
- **Database:** 2 vCPU, 4 GB RAM and 20 GB of storage cover 30 to 40 repositories. Embeddings are the
  largest table.

## Configuration essentials

Configuration comes from defaults, then an optional `dth.yaml` (`DTH_CONFIG`), then environment variables.
Unknown keys fail at start.

| Variable | What |
|---|---|
| `DTH_DATABASE_URL` | PostgreSQL URL (`?sslmode=require` in the cloud) |
| `DTH_PUBLIC_URL` | The address people use; webhooks and the SSO callback are under it |
| `DTH_ENVIRONMENT` | This deployment's name, e.g. `nonlive` or `production`; anything but production shows a banner ([environments.md](environments.md)) |
| `DTH_ROLES` | `api`, `worker`, `scheduler` |
| `DTH_LISTEN`, `DTH_METRICS_LISTEN` | API listener (default `127.0.0.1:8080`); `/metrics` and `/healthz` listener (default `127.0.0.1:9090`) |
| `DTH_SECRETS_PROVIDER` | `localfile` (with `DTH_LOCAL_KEY_FILE`), `awskms` or `gcpkms` (with `DTH_KMS_KEY_ID`) |
| `DTH_AUTH_MODE` | `local` (passwords on by default) or `oidc` (with `DTH_OIDC_*`); sign-in can also be set in the UI or the settings file |
| `DTH_SETTINGS_FILE`, `DTH_SETTINGS` | Settings applied on start ([settings-file.md](settings-file.md#applied-when-the-hub-starts)) |
| `DTH_SMTP_URL`, `DTH_EMAIL_FROM` | Email invite and password links ([users-and-sign-in.md](users-and-sign-in.md#email)); without them admins copy the link |
| `DTH_DOCS_VERSION` | `2` (default): documents per repository, kept in the Hub; `1`: one doc per source file, landed as a docs PR ([docs-generation.md](docs-generation.md)) |
| `DTH_DOCS_MODE` | Docs v1 only: cost mode `thorough`, `balanced` (default) or `economy` |
| `DTH_ASK_SIMILAR_ANSWER` | How alike a reworded question must be to reuse a cached answer (default 0.95, 0 = off; needs an embedding route) |
| `DTH_ASK_AGENT_STEPS` | How far Ask may look when search finds too little (default 4, 0 = off) |
| `DTH_ASK_SIFT`, `DTH_ASK_SIFT_KEEP_AT` | Ask's source picker: a cheap judge keeps only the sources an answer needs (`on` by default; skips itself when it would not save) ([ask-sources.md](ask-sources.md)) |
| `DTH_VECTOR_BACKEND` | `pgvector` (default) or `qdrant` (`DTH_QDRANT_URL`) |
| `DTH_LOG_LEVEL`, `DTH_LOG_FORMAT` | `info`/`debug`; `json` (default) or `text` |
| `DTH_TRACING_ENABLED`, `DTH_OTLP_ENDPOINT` | OpenTelemetry traces (off by default) |

## Monitoring

**Health checks:**
- `GET /healthz`: liveness. It is on the API listener and, for every role, on the metrics listener.
- `GET /readyz`: readiness. It checks the database, model routing and the vector index.

Point load balancer and orchestrator probes at these.

**Metrics:** Prometheus format at `/metrics` on `DTH_METRICS_LISTEN`. The Helm chart has a ServiceMonitor.

| Metric | Watch for |
|---|---|
| `dth_http_requests_total{route,code}`, `dth_http_duration_seconds` | 5xx rate, latency |
| `dth_queue_depth{type}`, `dth_jobs_in_flight{type}` | Backlog: docs taking long to appear |
| `dth_jobs_total{type,status}`, `dth_job_duration_seconds{type}` | `failed`, `dead`, `spend_blocked` jobs |
| `dth_llm_calls_total{feature,provider,outcome}` | `error` (keys, credits, outages) and `blocked` (spend limits) |
| `dth_llm_tokens_total{feature,provider,direction}` | Usage by feature; `cache_read` shows prompt caching working |
| `dth_llm_call_duration_seconds` | A slow provider |
| `dth_retrieval_duration_seconds{stage}` | Ask latency by stage |
| `dth_ingress_events_total{source,outcome}`, `dth_signal_pending_windows` | Signal intake, backpressure |

**Alerts:** ready-made rules are in
[`deploy/monitoring/alerts.yaml`](../deploy/monitoring/alerts.yaml). They cover:
- the Hub down, and API errors;
- a queue backlog, and failing jobs;
- failing model calls, and spend limits blocking calls;
- slow Ask;
- signal backpressure, and refused signal events.

For the Prometheus Operator, wrap the file in a PrometheusRule:

```sh
kubectl create configmap dth-alerts --from-file=deploy/monitoring/alerts.yaml   # plain Prometheus, or:
yq '{"apiVersion":"monitoring.coreos.com/v1","kind":"PrometheusRule","metadata":{"name":"dth"},"spec":.}' \
  deploy/monitoring/alerts.yaml | kubectl apply -f -
```

**In the product:**
- **Activity** shows every job with its progress and errors, plus recent events.
- **Usage** shows usage, cost and savings.

Both read the same tables the metrics describe.

**Logs:** JSON on stdout. Each line carries `correlation_id` and, where relevant, `job_id`, `repo` and
`user_id`. Every error response includes its `correlation_id`, so you can search the logs for it.

## Backups

Two things together make a complete backup. Neither is useful without the other:

1. **The database.** It holds everything: users, settings, docs, the index, encrypted secrets and the
   job queue.
2. **The master key.** It encrypts provider keys, connector credentials, the SSO client secret and the
   sealing key.

| Master key | What to back up |
|---|---|
| `localfile` | The key file (`DTH_LOCAL_KEY_FILE`; quickstart: `~/.dth-quickstart/master.key`, `dth up`: the `hubdata` volume). Store a copy offline, apart from the database backup. |
| `awskms`, `gcpkms` | Nothing to copy, but **never schedule the key for deletion**. Keep deletion protection on. Automatic rotation is safe (old key versions keep decrypting). |

**Rotating the master key** (or moving from a key file to KMS, or between KMS keys). `dth-hub rotate-key`
re-wraps every stored secret's data key under the new key in one transaction; the secrets themselves are
never decrypted. Run it where the Hub runs, with the Hub's own environment:

1. Back up the database and the current key.
2. Stop the Hub (`docker compose stop hub`, or scale the deployments to 0), so nothing saves a secret
   under the old key meanwhile. If something did, running the command again moves it.
3. Run one of:
   ```sh
   dth-hub rotate-key --dry-run --to-key-file /data/master-2.key   # counts what would move
   dth-hub rotate-key --to-key-file /data/master-2.key             # a new local key file (created 0600)
   DTH_NEW_LOCAL_KEY=$(openssl rand -base64 32) dth-hub rotate-key # Kubernetes: a key for a new Secret
   dth-hub rotate-key --to-awskms arn:aws:kms:…:key/…               # or --to-gcpkms projects/…/cryptoKeys/…
   ```
   For example `docker compose run --rm hub /dth-hub rotate-key --to-key-file /data/master-2.key`.
4. Point the Hub at the new key (`DTH_LOCAL_KEY_FILE`, the `DTH_LOCAL_KEY` Secret, or
   `DTH_SECRETS_PROVIDER` and `DTH_KMS_KEY_ID`; the command prints which) and start it.
5. Once it starts cleanly, back up the new key and destroy the old one.

The browser sealing key is separate: rotate it with `POST /api/v1/seal/rotate` ([security.md](security.md)).

**Database backups:**
- **Managed (RDS, Cloud SQL):** keep automated backups and point-in-time recovery on. The Terraform
  modules enable them (`db_backup_retention_days`, Cloud SQL backups). Take a manual snapshot before every
  upgrade.
- **Self-managed:** run nightly
  `pg_dump --format=custom --no-owner "$DTH_DATABASE_URL" > dth-$(date +%F).dump`, then copy the file off
  the machine.
- **Settings files:** keep them in git. They are the quickest way to rebuild configuration, and they
  never contain secret values.

### Restore

1. Stop the Hub (all roles), so no job writes during the restore.
2. Restore the database:
   - **Managed:** restore the snapshot or point in time to a new instance, then point
     `DTH_DATABASE_URL` at it.
   - **Self-managed:**
     ```sh
     createdb -h <host> -U <user> dth_restored
     psql -h <host> -U <user> -d dth_restored -c 'CREATE EXTENSION IF NOT EXISTS vector'
     pg_restore --no-owner -h <host> -U <user> -d dth_restored dth-2026-10-01.dump
     ```
3. Make sure the Hub uses the **same master key** as when the backup was taken (same key file, or the
   same KMS key ID).
4. Start the Hub. It migrates the schema forward if the backup is older, re-queues jobs that were
   running, and serves.
5. Check:
   - `/readyz` is green;
   - you can sign in;
   - **Settings → AI models → Test** succeeds (the secrets decrypt);
   - **Activity** shows jobs moving.

**If the master key is lost**, the database is still usable: docs, the index, users and history are
intact. Only the encrypted secrets are gone. Start with a new key, then enter again:
- the provider keys;
- the connector credentials;
- the SSO client secret.

Re-applying your settings file does all of this at once. Reconnect the GitHub App through **Connect with
GitHub → use an existing app**.

## Upgrades

The Hub migrates its schema on start, and migrations only add to the schema. Each release's notes say if
anything needs attention.

1. Read the release notes. Take a database snapshot (and confirm the master-key backup).
2. **One replica, or a short pause is fine:** deploy the new image. It migrates, then serves.
3. **Zero downtime with several replicas:**
   1. Run the migration once with the new image's CLI: `dth migrate --database-url "$DTH_DATABASE_URL"`.
   2. Roll the api, worker and scheduler replicas. Old replicas keep working against the migrated
      schema until they are replaced.
4. Confirm `/readyz`, sign in, and watch `DTHJobsFailing` and `DTHHTTPErrors` for a while.

**Where each deployment upgrades:**

| Deployment | Upgrade |
|---|---|
| quickstart | `git pull && ./scripts/quickstart.sh` |
| `dth up` | `dth up --upgrade` |
| Compose | change `DTH_IMAGE`, then `docker compose up -d` |
| Helm | `helm upgrade … --set image.tag=<new>` |
| Terraform | set `image`, then `terraform apply` |

**Rolling back:**
- **The release did not migrate the schema** (its notes say so): deploy the previous image.
- **It did:** restore the snapshot from step 1. Down migrations exist for development, but are not a
  supported way back with data in place.

## Security operations

- **Sessions and tokens:** disabling or removing a user ends their sessions and stops their tokens
  immediately. Personal access tokens expire as set when created; owners and admins see who has which in
  the audit log.
- **Audit log:** every change to users, sign-in, connectors, providers and settings is recorded. Read it at
  `GET /api/v1/audit` (admin), filtered by actor, action and time.
- **Secrets:** they are write-only in the UI and API, and sealed in the browser to the Hub's key. To rotate
  a provider key, enter the new one under **Settings → AI models** (or re-apply the settings file).
- **Retention:** signal events are kept for `retention.event_days` (30), and deleted chunks for
  `retention.chunk_gc_days` (14). Cached answers expire after 7 days or when their sources change.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Docs stay "queued" | No worker role running, or one repository's earlier job still running (one job per repository at a time) | Check `DTH_ROLES` includes `worker`; **Activity** shows progress |
| Docs job failed: "does not support the effort parameter" | An old version with a Haiku route | Upgrade; the Hub now leaves effort out for models that reject it |
| Ask says "I could not find this" | Nothing relevant indexed, or the person cannot see that repository | Check the repository has docs; with the agent on, the steps shown say what was searched |
| SSO: "single sign-on failed" | Callback URL not registered exactly, wrong secret, or no `email` claim (Entra: add the optional claim) | **Settings → People → Sign-in** shows the callback URL; logs show the IdP's error |
| SSO: "email domain is not allowed" | `allowed_domains` excludes it | Add the domain under **Settings → People → Sign-in** |
| Locked out (passwords off, SSO broken) | The identity provider changed, or its app was deleted | Restart the Hub with `DTH_SETTINGS='auth: {password: true}'`, then run `dth-hub invite you@acme.com` where the Hub runs (`docker compose exec hub /dth-hub invite …`, `kubectl exec deploy/dth-api -- /dth-hub invite …`). Open the printed link, set a password, sign in, and fix SSO under **Settings → People → Sign-in** |
| Webhooks not arriving | Hub not reachable from the git host, or `DTH_PUBLIC_URL` wrong | The Hub polls every minute as a fallback; check the delivery log on the git host |
| "master key … missing" at start | The key file is not where `DTH_LOCAL_KEY_FILE` points | Restore it from backup (see Backups); never start a new key over an existing database unless you accept re-entering secrets |
| Startup log: "settings at startup not fully applied" | A reference did not resolve, or the identity provider was unreachable | The log names the item; the Hub retries every 30 s and keeps serving |

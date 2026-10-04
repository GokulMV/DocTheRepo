# Install and deploy

For plain, step-by-step instructions (SSO, model keys, variables, every target), read
[deploy-guide.md](deploy-guide.md). This page is the shorter reference.

## Before you deploy

Run `dth init` first (`make build`, then `./bin/dth init`). It asks:
- where the Hub runs and its address;
- how people sign in (SSO and/or passwords);
- who the owners are;
- which model and git host it uses.

It writes a settings file and prints the prerequisites, the secrets to set and the deploy command for
your target. The Hub applies the file on every start, so sign-in, owners, models and repositories are in
place before anyone opens it. Prerequisites per target are in the [README](../README.md#prerequisites),
and the ways to pass the file are in [settings-file.md](settings-file.md#applied-when-the-hub-starts).

## From source, fully automatic (one command)

```sh
git clone https://github.com/GokulMV/DocTheRepo && cd DocTheRepo && ./scripts/quickstart.sh
```

Checks and installs what is missing, unattended: git, curl, tar, make, openssl, a C compiler, Go 1.25.13
and Node.js 22.12+ (private copies under `~/.dth-quickstart/toolchain` when the system ones are missing or too
old), and Docker + Compose (Linux: get.docker.com; macOS: Homebrew + Colima). It then builds the UI and the
hub (`make release`), starts PostgreSQL/pgvector in Docker, runs the hub, creates the owner account, and
opens a one-time link where you choose the owner password. No password is printed or stored; later runs use an
API token the first run created (revoke it under **Account**), and `--reset-password` prints a new link.
Set `ANTHROPIC_API_KEY` and/or `OPENAI_API_KEY` first to have the model routes configured too. Stop with
`./scripts/quickstart.sh --down` (`--wipe` deletes the data); `--container` builds and runs the container
image instead of using a host toolchain; `--help` lists every option. See
[docs/architecture.md](docs/architecture.md) for how the pieces fit, and [docs/opencode.md](docs/opencode.md) to
use opencode as the doc engine or to give opencode / Claude Code / Cursor the Hub as an MCP server (`dth mcp`).

## Local (one command)

Requires Docker. `dth up` writes `~/.dth/compose.yaml`, starts PostgreSQL (pgvector) and the hub, creates
the owner account, opens a one-time link to choose its password (no password is printed), and signs the CLI in. Re-running it is the health check;
`dth up --upgrade` pulls newer images.

```sh
make build                      # or download a dth release binary
./bin/dth up --owner-email you@acme.com
./bin/dth status
./bin/dth ask "how are refunds retried?" --repo acme/payments
./bin/dth down                  # --wipe also deletes the database and keys
```

Add `--ollama` to also run Ollama for local models. The UI is at <http://localhost:8080>.

## AWS — `deploy/terraform/aws`

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

You can also set up single sign-on later, in the Hub under **Sign-in & SSO**, and add people under
**Users & access**. See [Users and sign-in](users-and-sign-in.md).

## GCP — `deploy/terraform/gcp`

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

## Kubernetes — `deploy/helm/dth`

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

## Watched accounts — `deploy/terraform/modules/readonly-roles`

Apply `aws/` in each AWS account and `gcp/` in each GCP project the Hub should read from. Everything
granted is read-only (logs, alarms, metrics, queue/topic/stream metadata); SQS message peeking is opt-in,
and Pub/Sub dead-letter sampling uses Hub-owned subscriptions only.

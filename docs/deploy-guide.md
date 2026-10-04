# Deploy DocTheRepo Hub: step by step

This guide takes you from nothing to a running Hub that your team signs in to with your company login.
Follow the steps in order. Each one says exactly what to click or type. You need no programming knowledge,
only the ability to copy commands into a terminal.

**Time:** about 1–2 hours the first time. Most of it is waiting for accounts and builds.

---

## Step 1: Choose where to run it

| Option | Pick it when | Monthly cost (rough) | Skill needed |
|---|---|---|---|
| **A. One server** (any cloud: AWS EC2, Google Compute Engine, Azure VM, DigitalOcean…) | Most teams. Simplest and cheapest. | One VM: ~$20–40 | Copy commands |
| **B. AWS, fully managed** (Terraform) | Your company standardises on AWS and wants high availability | ~$150+ | Some AWS knowledge |
| **C. Google Cloud, fully managed** (Terraform) | Your company standardises on Google Cloud | ~$120+ | Some Google Cloud knowledge |
| **D. Kubernetes** (Helm) | You already run Kubernetes | Your cluster | Kubernetes knowledge |
| **E. Automated from GitHub** (nonlive + production) | You want every change deployed by a pipeline, with approval for production | Your cloud | Builds on B, C or D |
| **Just try it** on your laptop | You want to look before deciding | Free | Copy one command |

Model usage (Claude, OpenAI…) is billed separately by the provider, and the Hub enforces the limit you set.

> **Just trying it?** On a Mac or Linux laptop run
> `git clone https://github.com/GokulMV/DocTheRepo && cd DocTheRepo && ./scripts/quickstart.sh`.
> It installs what is missing and opens the Hub in your browser. You can skip the rest of this guide.

---

## Step 2: Collect what you need

Copy this worksheet into a note. You will fill it in during this step and use it in step 3.

| # | What | Your value |
|---|---|---|
| 1 | Hub address | `https://docs-hub.yourcompany.com` |
| 2 | SSO issuer URL | |
| 3 | SSO client ID | |
| 4 | SSO client secret | *(secret: keep it in a password manager)* |
| 5 | Allowed email domain(s) | `yourcompany.com` |
| 6 | Owner email(s) | |
| 7 | Anthropic API key | *(secret)* |
| 8 | OpenAI API key | *(secret, optional but recommended)* |
| 9 | Daily AI budget (USD) | e.g. `50` |
| 10 | Email sending (optional) | SMTP URL *(secret)* and From address |

### 2.1 The Hub address (row 1)

Pick a name such as `docs-hub.yourcompany.com`. Ask whoever manages your company's domain to create it.
For now, just write the name down: they will point it at the server in step 4.

### 2.2 Single sign-on app (rows 2–5)

People sign in with their existing company account. Create one "app" in your identity provider. If you
are not an admin there, send this section to your IT team.

The **callback URL** (also called redirect URI) to enter in every provider is:

```
https://docs-hub.yourcompany.com/api/v1/auth/callback
```

(your Hub address from row 1, followed by `/api/v1/auth/callback`)

<details open><summary><b>Google Workspace</b></summary>

1. Open <https://console.cloud.google.com/apis/credentials>. Pick or create a project.
2. **OAuth consent screen** → User type **Internal** → fill in the app name → Save.
3. **Credentials → Create credentials → OAuth client ID** → Application type **Web application**.
4. Under **Authorized redirect URIs**, add the callback URL → **Create**.
5. Copy the **Client ID** (row 3) and **Client secret** (row 4).
6. Row 2 issuer: `https://accounts.google.com`.
</details>

<details><summary><b>Microsoft Entra ID (Azure AD / Microsoft 365)</b></summary>

1. Open <https://entra.microsoft.com> → **Applications → App registrations → New registration**.
2. Name it, choose **Accounts in this organizational directory only**.
3. Redirect URI: platform **Web**, paste the callback URL → **Register**.
4. Copy **Application (client) ID** (row 3) and **Directory (tenant) ID**.
5. **Certificates & secrets → New client secret** → copy the **Value** (row 4).
6. **Token configuration → Add optional claim → ID → email** → Add.
7. Row 2 issuer: `https://login.microsoftonline.com/<Directory (tenant) ID>/v2.0`.
</details>

<details><summary><b>Okta</b></summary>

1. Okta Admin → **Applications → Create App Integration** → **OIDC** → **Web Application**.
2. **Sign-in redirect URIs**: paste the callback URL → Save.
3. **Assignments**: assign the people or groups who may use the Hub.
4. Copy **Client ID** (row 3) and **Client secret** (row 4).
5. Row 2 issuer: `https://<your-org>.okta.com`.
</details>

<details><summary><b>Keycloak</b></summary>

1. Your realm → **Clients → Create client** → OpenID Connect → turn on **Client authentication**.
2. **Valid redirect URIs**: paste the callback URL → Save.
3. **Credentials** tab → copy the client secret (row 4). The client ID is what you named it (row 3).
4. Row 2 issuer: `https://<keycloak-host>/realms/<realm>`.
</details>

Row 5: the email domain(s) allowed to sign in, for example `yourcompany.com`. **Always set this.**
Row 6: the people who will run the Hub. They are owners from their first sign-in, and no password is
ever created.

### 2.3 AI model keys (rows 7–8)

| Provider | Where to get the key | Used for |
|---|---|---|
| **Anthropic (Claude)**, recommended | <https://console.anthropic.com/settings/keys> → **Create key**. Add billing under **Plans & billing**. | Writing docs, answering questions, explaining errors |
| **OpenAI**, recommended | <https://platform.openai.com/api-keys> → **Create new secret key**. Add billing. | Search by meaning (embeddings) |
| Others (optional) | Azure OpenAI, AWS Bedrock, Google Vertex AI, GitHub Models, Ollama, TypeSafe Jev | Add them later in the Hub under **Providers & routing**; each one shows where to get its key |

One provider is enough to start. Without OpenAI, search falls back to word matching.

### 2.4 Email (row 10, optional)

The Hub can email invitation links. Without email, an admin copies the link and sends it themselves. If
you want email, get an SMTP login from your mail provider (SendGrid, Amazon SES, Google Workspace,
Microsoft 365…). Row 10 is written like `smtp://USER:PASSWORD@smtp.sendgrid.net:587`, plus a From address
like `DocTheRepo <docs@yourcompany.com>`.

---

## Step 3: Fill in the settings file

The settings file tells the Hub, at its first start, who signs in, who owns it, which AI models to use and
how much it may spend. It contains **no secrets**, only their names, so it is safe to keep in git.

1. Get the code: `git clone https://github.com/GokulMV/DocTheRepo && cd DocTheRepo`
2. Copy the template: `cp deploy/templates/hub.yaml hub.yaml`
3. Open `hub.yaml` in any text editor and change every line marked `CHANGE`:

| In the file | Put |
|---|---|
| `provider:` | `google`, `microsoft`, `okta` or `keycloak` |
| `issuer:` | Row 2 |
| `client_id:` | Row 3 |
| `allowed_domains:` | Row 5, e.g. `[yourcompany.com]` |
| `users:` → `email:` | Row 6. Add one line per owner or admin. |
| `max_cost_usd:` | Row 9 |

4. If you have no OpenAI key, delete the `openai` provider block and the `embedding:` line.

Leave the `${env:DTH_SECRET_…}` parts exactly as they are. Those names are filled from the secrets you
set in step 4:

| Name in the file | Value |
|---|---|
| `DTH_SECRET_OIDC_CLIENT_SECRET` | Row 4 |
| `DTH_SECRET_ANTHROPIC_API_KEY` | Row 7 |
| `DTH_SECRET_OPENAI_API_KEY` | Row 8 |

---

## Step 4: Deploy

Follow **one** of the sections below.

### A. One server (any cloud)

**4A.1 Create the server**

- **Size:** 2 vCPU, 4 GB memory, 30 GB disk. **Operating system:** Ubuntu 24.04.
  - AWS: EC2 → Launch instance → `t3.medium`, Ubuntu 24.04.
  - Google Cloud: Compute Engine → Create instance → `e2-medium`, Ubuntu 24.04.
  - Azure: Virtual machine → `B2s`, Ubuntu 24.04. DigitalOcean: Droplet, 4 GB.
- **Firewall / security group:** allow inbound ports **22** (SSH), **80** and **443** (web).
- Ask your domain admin to create an **A record**: `docs-hub.yourcompany.com` → the server's public IP.

**4A.2 Install Docker and build the Hub.** Connect to the server (SSH) and run:

```sh
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER && newgrp docker
git clone https://github.com/GokulMV/DocTheRepo && cd DocTheRepo
docker build -f docker/Dockerfile -t doctherepo-hub:local .     # takes 10–15 minutes
```

**4A.3 Add your settings and secrets.** Copy your `hub.yaml` from step 3 to the server, for example by
pasting it into `nano ~/dth/settings/hub.yaml` after the first two commands below. Then:

```sh
mkdir -p ~/dth/settings
cp deploy/compose/docker-compose.yml ~/dth/
# put your hub.yaml in ~/dth/settings/hub.yaml
cd ~/dth
nano .env
```

Paste this into `.env`, replacing the values with your worksheet rows:

```sh
DTH_IMAGE=doctherepo-hub:local
DTH_PUBLIC_URL=https://docs-hub.yourcompany.com
DTH_DB_PASSWORD=PUT-A-LONG-RANDOM-PASSWORD-HERE
DTH_SECRET_OIDC_CLIENT_SECRET=row-4
DTH_SECRET_ANTHROPIC_API_KEY=row-7
DTH_SECRET_OPENAI_API_KEY=row-8
# optional email (row 10):
# DTH_SMTP_URL=smtp://USER:PASSWORD@smtp.example.com:587
# DTH_EMAIL_FROM=DocTheRepo <docs@yourcompany.com>
```

Save (Ctrl+O, Enter, Ctrl+X). For a random password, `openssl rand -hex 24` prints one. Protect the file:
`chmod 600 .env`.

**4A.4 Check the settings, then start:**

```sh
docker run --rm --entrypoint /dth -v "$PWD/settings:/s:ro" doctherepo-hub:local settings check -f /s/hub.yaml --require-owner
docker compose up -d
```

The check must end with `No problems found.` If it doesn't, fix what it lists in `hub.yaml`.

**4A.5 Turn on HTTPS.** This uses Caddy, which gets and renews the certificate for you:

```sh
sudo apt-get install -y caddy
echo 'docs-hub.yourcompany.com {
  reverse_proxy 127.0.0.1:8080
}' | sudo tee /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Open `https://docs-hub.yourcompany.com`, then go to step 5.

**Back up the master key now.** It decrypts every stored key, and without it a restored database is
useless:
`docker compose cp hub:/data/master.key ./master.key.backup`. Store the copy in your password manager,
not on the server.

### B. AWS, fully managed (Terraform)

What it creates: a load balancer with HTTPS, the Hub on ECS Fargate (2 API, 2 worker, 1 scheduler), a
Multi-AZ PostgreSQL database, a KMS key and Secrets Manager entries.

**You need:** an AWS account with admin rights, [Terraform 1.6+](https://developer.hashicorp.com/terraform/install),
the [AWS CLI](https://aws.amazon.com/cli/) signed in (`aws configure`), and Docker.

1. **Network.** Use a VPC with 2 public and 2 private subnets and a NAT gateway (VPC console → **Create
   VPC → VPC and more**, 2 AZs, 1 NAT gateway). Note the VPC ID and subnet IDs.
2. **Certificate.** ACM console → **Request certificate** → your Hub address → DNS validation → add the
   record it shows. Note its ARN.
3. **Image.** Build it and push it to a private ECR repository:
   ```sh
   aws ecr create-repository --repository-name doctherepo-hub
   ACCOUNT=$(aws sts get-caller-identity --query Account --output text); REGION=us-east-1
   aws ecr get-login-password --region $REGION | docker login --username AWS --password-stdin $ACCOUNT.dkr.ecr.$REGION.amazonaws.com
   docker build -f docker/Dockerfile -t $ACCOUNT.dkr.ecr.$REGION.amazonaws.com/doctherepo-hub:v1 .
   docker push $ACCOUNT.dkr.ecr.$REGION.amazonaws.com/doctherepo-hub:v1
   ```
4. **Variables.**
   ```sh
   cd deploy/terraform/aws
   cp terraform.tfvars.example terraform.tfvars
   ```
   Edit `terraform.tfvars`: region, VPC and subnet IDs, `domain_name` (row 1), `acm_certificate_arn`,
   `route53_zone_id` (if your domain is in Route 53; otherwise leave it empty), `image` (from 3.),
   `owner_email` (row 6), `oidc_issuer` (row 2), `oidc_client_id` (row 3), `oidc_allowed_domains` (row 5).
5. **Secrets and settings**, which go into Secrets Manager and never into a file:
   ```sh
   export TF_VAR_oidc_client_secret='row-4'
   export DTH_SECRET_OIDC_CLIENT_SECRET='row-4' DTH_SECRET_ANTHROPIC_API_KEY='row-7' DTH_SECRET_OPENAI_API_KEY='row-8'
   export TF_VAR_hub_settings="$(docker run --rm --entrypoint /dth -v "$PWD/../../../hub.yaml:/hub.yaml:ro" \
     -e DTH_SECRET_OIDC_CLIENT_SECRET -e DTH_SECRET_ANTHROPIC_API_KEY -e DTH_SECRET_OPENAI_API_KEY \
     $ACCOUNT.dkr.ecr.$REGION.amazonaws.com/doctherepo-hub:v1 settings resolve -f /hub.yaml)"
   ```
6. **Deploy:** `terraform init && terraform apply` and type `yes`. This takes about 15 minutes.
7. If your domain is not in Route 53, create a CNAME from your Hub address to the `alb_dns_name` output.

Go to step 5.

### C. Google Cloud, fully managed (Terraform)

What it creates: an HTTPS load balancer with a Google-managed certificate, the Hub on Cloud Run, Cloud SQL
PostgreSQL (high availability), Cloud KMS and Secret Manager.

**You need:** a Google Cloud project with billing, [Terraform 1.6+](https://developer.hashicorp.com/terraform/install),
the [gcloud CLI](https://cloud.google.com/sdk/docs/install) signed in (`gcloud auth application-default login`),
and Docker.

1. **Image.** Cloud Run needs it in Artifact Registry:
   ```sh
   PROJECT=your-project; REGION=us-central1
   gcloud artifacts repositories create dth --repository-format=docker --location=$REGION
   gcloud auth configure-docker $REGION-docker.pkg.dev
   docker build -f docker/Dockerfile -t $REGION-docker.pkg.dev/$PROJECT/dth/doctherepo-hub:v1 .
   docker push $REGION-docker.pkg.dev/$PROJECT/dth/doctherepo-hub:v1
   ```
2. **Variables.** `cd deploy/terraform/gcp && cp terraform.tfvars.example terraform.tfvars`, then edit
   `project_id`, `region`, `network`/`subnetwork` (your VPC; `default` works), `domain_name`, `image`,
   `owner_email`, `oidc_issuer`, `oidc_client_id` and `oidc_allowed_domains`.
3. **Secrets and settings:** the same commands as AWS step 5, with your Artifact Registry image name.
4. **Deploy:** `terraform init && terraform apply` and type `yes`.
5. **DNS:** create an A record from your Hub address to the `load_balancer_ip` output. The certificate
   becomes active 15–60 minutes later.

Go to step 5.

### D. Kubernetes (Helm)

**You need:** Kubernetes 1.27+, Helm 3, PostgreSQL 16 with the pgvector extension, an Ingress controller
with TLS, and the image in a registry your cluster can pull from.

```sh
kubectl create namespace dth
kubectl -n dth create secret generic dth-db   --from-literal=url='postgres://USER:PASSWORD@HOST:5432/dth?sslmode=require'
kubectl -n dth create secret generic dth-oidc --from-literal=client-secret='row-4'
kubectl -n dth create secret generic dth-app  --from-literal=DTH_SECRET_ANTHROPIC_API_KEY='row-7' --from-literal=DTH_SECRET_OPENAI_API_KEY='row-8'
kubectl -n dth create secret generic dth-key  --from-literal=master-key="$(openssl rand -base64 32)"   # back this up

helm install dth deploy/helm/dth -n dth \
  --set image.repository=YOUR-REGISTRY/doctherepo-hub --set image.tag=v1 \
  --set publicURL=https://docs-hub.yourcompany.com \
  --set database.existingSecret=dth-db \
  --set secrets.provider=localfile --set secrets.localKey.existingSecret=dth-key \
  --set auth.mode=oidc --set auth.oidc.issuer=ROW-2 --set auth.oidc.clientId=ROW-3 \
  --set auth.oidc.existingSecret=dth-oidc --set 'auth.oidc.allowedDomains={yourcompany.com}' \
  --set 'extraEnvFrom[0].secretRef.name=dth-app' \
  --set-file settings.inline=hub.yaml \
  --set ingress.enabled=true --set ingress.className=nginx
```

On AWS or Google Cloud, prefer `secrets.provider=awskms` or `gcpkms` with workload identity over a key
Secret. See [install.md](install.md#kubernetes--deployhelmdth).

### E. Automated from GitHub (nonlive and production)

Every merge deploys to a test environment ("nonlive"). Production gets the same image after someone
approves.

1. **Fork** the DocTheRepo repository (or copy it into your organization).
2. **Build the image from GitHub:** copy `deploy/github-actions/build-image.yml` into `.github/workflows/`,
   commit, then **Actions → build-hub-image → Run workflow** with tag `v1.0.0`. It publishes
   `ghcr.io/<your-org>/doctherepo-hub:v1.0.0`. Under the package's settings, give your cluster access or
   make it public.
3. **Copy the deploy workflows:** `deploy/github-actions/deploy-hub.yml` and `deploy-hub-env.yml` into
   `.github/workflows/`, and fill in `deploy/environments/nonlive/` and `production/` (a `values.yaml`
   and a `settings.yaml` each: copy your `hub.yaml` there).
4. **Create two GitHub environments:** repository **Settings → Environments → New environment**:
   `nonlive` and `production`. On `production`, tick **Required reviewers** and add who may approve.
5. **On each environment, add variables** (**Add variable**):

   | Variable | Example |
   |---|---|
   | `HUB_URL` | `https://docs-hub.yourcompany.com` (production) / `https://docs-hub-nonlive.yourcompany.com` |
   | `DEPLOY_ROLE_ARN` | The AWS role GitHub may assume, one per environment |
   | `AWS_REGION` | `us-east-1` |
   | `EKS_CLUSTER` | Your cluster name |
   | `SECRETS_FROM` | `github` (simplest) or `cloud` |

6. **With `SECRETS_FROM=github`, add secrets** on each environment (**Add secret**): `DATABASE_URL`,
   `OIDC_CLIENT_SECRET`, `DTH_SECRET_ANTHROPIC_API_KEY`, `DTH_SECRET_OPENAI_API_KEY`, and optionally
   `SMTP_URL`.
7. Add the repository variable `HUB_IMAGE` = the image from 2, then **Actions → deploy-hub → Run workflow**.

Before deploying, the pipeline checks that every variable and secret exists, that the settings file names
an owner, and that every secret resolves. If something is missing, it stops and names it. Each environment
needs its own SSO app (its own callback URL). Full details: [environments.md](environments.md#deploying-from-github).

---

## Step 5: First sign-in and finishing touches

1. Open your Hub address and click **Sign in with single sign-on**. Use an owner email from row 6.
2. **Connect your code:** **Connectors → Connect with GitHub**. GitHub opens: confirm creating the app,
   choose the repositories, then **Install**. For an organization, open **Organization or GitHub
   Enterprise** first and type its name. GitLab: add a GitLab connector with a token.
3. **Pick repositories:** **Repositories → Track repository**. Docs generation starts by itself.
4. **Check the models:** **Providers & routing** shows your providers. Click **Test** on each.
5. **Invite people:** anyone from your allowed domain can already sign in, as a viewer. To give someone
   another role, add them under **Users & access** first.
6. **Ask a question** on the **Ask** page.

### Check it works

| Check | Where | Expected |
|---|---|---|
| Hub is up | `https://<your Hub>/readyz` | `"status":"ready"` |
| SSO works | Sign in | You land on the Hub as owner |
| Models work | Providers & routing → Test | Green for each provider |
| Code connected | Repositories | Your repositories listed, docs being written |
| Answers work | Ask | An answer with numbered sources |
| Costs visible | Analytics | Usage, savings, Ask source picking |

---

## Reference: environment variables

You only need these if you deploy without the provided Compose file, Terraform or Helm chart. Those set
them for you.

| Variable | Required | What it is |
|---|---|---|
| `DTH_DATABASE_URL` | yes | PostgreSQL 16 with pgvector, e.g. `postgres://user:pass@host:5432/dth?sslmode=require` |
| `DTH_PUBLIC_URL` | yes | The Hub address (row 1); SSO callbacks and links are built from it |
| `DTH_SECRETS_PROVIDER` | yes | `localfile`, `awskms` or `gcpkms`: what encrypts stored keys |
| `DTH_LOCAL_KEY_FILE` / `DTH_KMS_KEY_ID` | one of | Master key file path, or the KMS key |
| `DTH_SETTINGS_FILE` or `DTH_SETTINGS` | recommended | Path to your settings file(s), or its contents |
| `DTH_SECRET_*` | as referenced | The secrets your settings file names |
| `DTH_AUTH_MODE` + `DTH_OIDC_ISSUER`, `DTH_OIDC_CLIENT_ID`, `DTH_OIDC_CLIENT_SECRET`, `DTH_OIDC_ALLOWED_DOMAINS` | optional | SSO from the environment instead of the settings file |
| `DTH_SMTP_URL`, `DTH_EMAIL_FROM` | optional | Email invitations |
| `DTH_ENVIRONMENT` | optional | `nonlive` shows a banner on every page; default `production` |
| `DTH_ROLES` | optional | `api`, `worker`, `scheduler` (default: all three in one process) |
| `DTH_ASK_SIFT` | optional | `off` disables Ask source picking (default `on`) |
| `DTH_LOG_LEVEL`, `DTH_LOG_FORMAT` | optional | `info`/`debug`; `json`/`text` |

Every option: [operations.md](operations.md). Every settings-file field: [settings-file.md](settings-file.md).

---

## Troubleshooting

| You see | Do this |
|---|---|
| SSO says "redirect URI mismatch" | The callback URL in your SSO app must be exactly `https://<Hub address>/api/v1/auth/callback`, and `DTH_PUBLIC_URL` must be that address |
| "Not allowed to sign in" | Your email's domain is not in `allowed_domains` |
| Signed in, but not an owner | Your email is not in `users:` with `role: owner`, or is spelled differently. Fix the file and restart. |
| `settings check` lists problems | Fix each line it names in `hub.yaml` |
| Page does not load | Server firewall must allow 80/443; DNS must point at the server (`nslookup <Hub address>`) |
| Models fail on **Test** | Check the key and that billing is set up at the provider |
| SSO broke and nobody can sign in | Run `docker compose exec hub /dth-hub invite you@yourcompany.com` (Kubernetes: `kubectl -n dth exec deploy/dth-api -- /dth-hub invite …`) for a one-time sign-in link. See [users-and-sign-in.md](users-and-sign-in.md#locked-out). |

## Keeping it running

- **Upgrade:** on the server, `cd DocTheRepo && git pull && docker build -f docker/Dockerfile -t doctherepo-hub:local . && cd ~/dth && docker compose up -d`.
  With Terraform or Helm, change the image tag and apply again. The database upgrades itself.
- **Back up:** the database (cloud setups back it up daily) and the master key (option A: the file you
  saved; Kubernetes: the `dth-key` Secret). See [operations.md](operations.md).
- **Change settings later:** owners can change sign-in, users, models and spend limits in the Hub itself
  (**Sign-in & SSO**, **Users & access**, **Providers & routing**). Edits to `hub.yaml` apply on the next
  restart; they create and update, never delete.

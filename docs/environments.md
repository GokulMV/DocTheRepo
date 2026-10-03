# Nonlive and production

How a team runs the Hub in two environments (call them nonlive and production) from one pipeline,
with separate logins and data, and how people move between the two.

## The model: same image, separate everything else

| | Nonlive | Production |
|---|---|---|
| Image | The release under test, pinned by digest | **The same digest**, promoted after nonlive passed |
| Cloud account or project | Its own (or at least its own namespace and roles) | Its own |
| Database | Its own PostgreSQL | Its own PostgreSQL |
| Master key | Its own KMS key | Its own KMS key |
| Sign-in | Its own SSO app (callback on the nonlive URL) | Its own SSO app |
| GitHub | Its own GitHub App or bot, on the test repositories or the integration branch | The production App |
| Model keys | Separate keys, tighter spend limits, cheaper models | Production keys |
| What it tracks | `tracked_branch: develop`, docs PRs wait for an approver | `tracked_branch: main`, auto-merged docs PRs |
| UI | A **"Nonlive environment: not production"** banner on every page, `[nonlive]` in the tab title | No banner |

Nothing is shared, so nonlive can be wiped, upgraded first, or broken by an experiment without touching
production. Configuration is the only thing that differs, and it lives in git:

```
deploy/environments/
  nonlive/values.yaml      Helm values: URL, environment name, database secret, KMS key, SSO app
  nonlive/settings.yaml    Hub settings: owners and admins, providers, routes, connectors, repositories, spend
  production/values.yaml
  production/settings.yaml
```

The settings files hold only references to secrets (`${awssm:production/dth/anthropic#api_key}`), so
they are safe to commit and review in a pull request. Each Hub resolves them at start with its own cloud
role, so secret values never pass through CI and the nonlive Hub cannot read production's secrets. See
[settings-file.md](settings-file.md) for every field.

## The pipeline

[`deploy/github-actions/deploy-hub.yml`](../deploy/github-actions/deploy-hub.yml) and
[`deploy-hub-env.yml`](../deploy/github-actions/deploy-hub-env.yml) are workflows to copy into your
infrastructure repository's `.github/workflows/`. They:

1. **Pins the image to a digest.** You start it with a tag (or it runs when `deploy/environments/**`
   changes on `main`); from then on only the digest is used.
2. **Checks before touching anything** (see [Deploying from GitHub](#deploying-from-github)): the GitHub
   Environment's variables, the settings file (an owner is listed, every secret reference resolves), and
   the cluster secrets the values file names.
3. **Deploys to nonlive** (`helm upgrade --wait --atomic` with nonlive's values and settings), then
   smoke-tests: `/readyz`, and `/api/v1/auth/config` reporting `environment: nonlive` with SSO on.
4. **Waits for approval.** The `production` GitHub Environment has required reviewers and only accepts
   the `main` branch.
5. **Deploys the same digest to production** with the same steps.

Cloud access uses GitHub's OIDC token. Each environment's cloud role trusts only its own GitHub
Environment, so the nonlive job cannot deploy to production even by mistake. There are no long-lived cloud
keys in GitHub.

Database migrations run when the new version starts. They are forward-only, so promote nonlive first and
keep the production database backups described in [operations.md](operations.md).

The Terraform modules take the same `environment` variable (`terraform apply -var-file=…/terraform.tfvars`)
if you deploy with Terraform instead of Helm.

## Deploying from GitHub

Everything a Hub needs at first boot (where it runs, how people sign in, who owns it, its secrets) comes
from three places. Nothing is typed in after deploying, and no password is created.

| What | Where it lives | Why there |
|---|---|---|
| Who owns and administers the Hub | `users:` in `deploy/environments/<env>/settings.yaml` | Changes are pull requests, reviewed and in git history. A listed owner owns the Hub from their first SSO sign-in, so nobody can claim it first |
| SSO app (issuer, client ID, allowed domains), public URL, KMS key | `deploy/environments/<env>/values.yaml` | Not secret; differs per environment |
| Cloud role, region, cluster, `HUB_URL` | GitHub Environment **variables** (Settings → Environments → `<env>`) | Tells the workflow where to deploy; per environment, so nonlive can never point at production |
| SSO client secret, database URL, SMTP URL | A cloud secret manager synced into cluster secrets (default), **or** GitHub Environment **secrets** | See the two options below |
| Model keys, git tokens, webhook secrets | References in `settings.yaml`: `${awssm:…}` / `${gcpsm:…}`, or `${env:DTH_SECRET_…}` | The file stays safe to commit |

### Variables to create on each GitHub Environment

| Name | Example (production) | Notes |
|---|---|---|
| `DEPLOY_ROLE_ARN` | `arn:aws:iam::222222222222:role/dth-deploy` | Trusts GitHub OIDC for `repo:acme/infra:environment:production` only |
| `AWS_REGION` | `eu-west-1` | |
| `EKS_CLUSTER` | `platform-prod` | |
| `HUB_URL` | `https://docs-hub.acme.example` | Must equal `publicURL` in values.yaml; the SSO callback is `HUB_URL/api/v1/auth/callback` |
| `SECRETS_FROM` | `cloud` | Optional: `cloud` (default) or `github` |

Plus the repository variable `HUB_IMAGE` (the default image), and on `production`: Required reviewers
and Deployment branches `main`.

### Secrets: two options

**`SECRETS_FROM=cloud` (recommended).** Secrets live in AWS Secrets Manager or Google Secret Manager.
External Secrets Operator (or your Terraform) creates the cluster secrets named in values.yaml
(`dth-db`, `dth-oidc`, `dth-smtp`), and the Hub reads `${awssm:…}` references with its own workload role.
GitHub holds no secret at all, only the role to assume. The workflow fails before deploying if a named
cluster secret is missing.

**`SECRETS_FROM=github` (simplest to start).** Put the secrets on the GitHub Environment:

| Secret | Required | Becomes |
|---|---|---|
| `DATABASE_URL` | yes | cluster secret `dth-github`, key `DATABASE_URL` |
| `OIDC_CLIENT_SECRET` | yes | cluster secret `dth-github`, key `OIDC_CLIENT_SECRET` |
| `SMTP_URL` | no | cluster secret `dth-github`, key `SMTP_URL` (invite emails) |
| `DTH_SECRET_<NAME>` | no | an environment variable on the Hub, used as `${env:DTH_SECRET_<NAME>}` in settings.yaml |

The workflow writes them to the cluster on every deploy and points the chart at them. Rotating a value means
updating the GitHub secret and re-running the workflow. GitHub masks them in logs. The cost: anyone who can
change the workflow on an allowed branch can read them, so protect the branch and the `production`
environment.

### What the preflight checks

The deploy stops, with a message naming the missing piece, when:

- a required variable (or, with `SECRETS_FROM=github`, a required secret) is not set on the environment;
- `values.yaml` has a different `environment:` or `publicURL` than the GitHub Environment;
- `settings.yaml` lists no owner, lists an owner or admin outside the SSO allowed domains, has a typo in a
  key, or has a secret reference that does not resolve. The pinned image's own CLI does this check, with the
  job's cloud role: `dth settings check -f settings.yaml --require-owner --resolve`. It prints reference
  names, never values. Run it locally before opening the pull request;
- a cluster secret named in `values.yaml` does not exist.

After deploying, the smoke test confirms the Hub reports the right environment and has SSO switched on.

### SSO: once per environment, outside GitHub

Register one SSO app per environment with your identity provider (Okta, Entra ID, Google Workspace),
with redirect URI `HUB_URL/api/v1/auth/callback`. Put the client ID in values.yaml and the client secret
in the secret store. This is the one manual step: identity providers do not let a pipeline create apps
without admin rights you should not give CI. If your IdP is managed with Terraform (`okta_app_oauth`,
`azuread_application`), create the apps there and output the client ID into values.yaml.

## Logins

- **People:** SSO only in both environments (`auth.password: false`), with a separate SSO app per
  environment. Owners and admins are listed per environment in `settings.yaml`. Nonlive can be wider
  (engineers as admins, so they can try things); production lists only the people who run it. Everyone else
  from the allowed domains signs in as a viewer.
- **No passwords in pipelines:** CI never sets `DTH_OWNER_PASSWORD`. The first owner signs in with SSO
  because they are listed in `settings.yaml`. If SSO breaks, an operator runs
  `kubectl exec deploy/dth-api -- /dth-hub invite you@acme.com owner` for a one-time link (see
  [users-and-sign-in.md](users-and-sign-in.md#locked-out)).
- **Tools and scripts:** use personal access tokens (Account → Personal access tokens), one per
  environment, kept in that environment's secret store.

## Moving between environments

- **In the browser:** the two Hubs have different URLs, and nonlive shows its banner on every page.
- **From the CLI:** save one profile per environment and switch explicitly:

  ```sh
  dth login --profile nonlive    --server https://docs-hub.nonlive.acme.example --token dth_pat_…
  dth login --profile production --server https://docs-hub.acme.example         --token dth_pat_…
  dth profile                      # * nonlive   https://docs-hub.nonlive…
  dth status --profile production  # one command against production
  dth profile use production       # switch the default
  ```

  In CI, set `DTH_PROFILE`, or `DTH_SERVER` and `DTH_TOKEN`, per job.
- **Promoting configuration:** change `deploy/environments/nonlive/settings.yaml` in a pull request,
  merge, and the pipeline applies it to nonlive. Then make the same change in `production/settings.yaml`.
  To start from what is running, `dth settings export --profile nonlive > settings.yaml` writes the live
  configuration as a file to compare or copy.
- **Data is not promoted.** Docs are regenerated from code in each environment, and questions and
  users stay where they were created.

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

[`deploy/github-actions/deploy-hub.yml`](../deploy/github-actions/deploy-hub.yml) is a workflow to copy
into your infrastructure repository. It:

1. **Pins the image to a digest.** You start it with a tag (or it runs when `deploy/environments/**`
   changes on `main`); from then on only the digest is used.
2. **Deploys to nonlive** (`helm upgrade --wait` with nonlive's values and settings), then smoke-tests:
   `/readyz`, and `/api/v1/auth/config` reporting `environment: nonlive`.
3. **Waits for approval.** The `production` GitHub Environment has required reviewers and only accepts
   the `main` branch.
4. **Deploys the same digest to production** with `--atomic` (a failed rollout rolls back), and
   smoke-tests it.

Cloud access uses GitHub's OIDC token. Each environment's cloud role trusts only its own GitHub
Environment, so the nonlive job cannot deploy to production even by mistake. There are no long-lived cloud
keys in GitHub.

Database migrations run when the new version starts. They are forward-only, so promote nonlive first and
keep the production database backups described in [operations.md](operations.md).

The Terraform modules take the same `environment` variable (`terraform apply -var-file=…/terraform.tfvars`)
if you deploy with Terraform instead of Helm.

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

<!-- dth:generated source="deploy/github-actions/deploy-hub-env.yml" — edit only inside dth:human blocks -->
# `deploy/github-actions/deploy-hub-env.yml`

<!-- dth:chunk d44a285206bc083b -->
## `deploy/github-actions/deploy-hub-env.yml`

This GitHub Actions workflow template deploys a single environment of the Hub application via Helm, called by a parent workflow for each target environment (nonlive/production). It validates prerequisites (environment variables, required files, secrets configuration), authenticates to AWS via OIDC, manages cluster secrets, deploys the Helm chart with configurable secret sources (cloud-managed or GitHub secrets), and runs smoke tests to verify the deployment succeeded and SSO is configured.

The workflow supports two secret management modes via the `SECRETS_FROM` variable: "cloud" assumes secrets already exist in the cluster (checked but not created), while "github" mode creates Kubernetes secrets from GitHub environment secrets and passes them to Helm. It requires environment variables `DEPLOY_ROLE_ARN`, `AWS_REGION`, `EKS_CLUSTER`, and `HUB_URL`, plus files `values.yaml` and `settings.yaml` in the environment-specific directory. When using GitHub secrets, `DATABASE_URL` and `OIDC_CLIENT_SECRET` are mandatory; `SMTP_URL` and custom `DTH_SECRET_*` variables are optional.

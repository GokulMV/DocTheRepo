<!-- dth:generated source="deploy/github-actions/deploy-hub.yml" — edit only inside dth:human blocks -->
# `deploy/github-actions/deploy-hub.yml`

<!-- dth:chunk b406ebb15a14a1d6 -->
## `deploy/github-actions/deploy-hub.yml`

### Overview
GitHub Actions workflow that deploys the DocTheRepo Hub to nonlive and then production environments. The workflow can be triggered manually with a custom image or automatically when `deploy/environments/**` paths change on main, ensuring settings changes deploy like releases.

### Setup
Copy this file to `.github/workflows/` in the infrastructure repository alongside `deploy/environments/` and `deploy-hub-env.yml`. Requires Kubernetes + Helm (chart at `charts/dth`) or Terraform. Configure two GitHub Environments:
- **nonlive**: Set variables `DEPLOY_ROLE_ARN`, `AWS_REGION`, `EKS_CLUSTER`, `HUB_URL` with no reviewers
- **production**: Same variables, plus required reviewers (who approve promotion) and deployment branch filter to `main`

Set repository variable `HUB_IMAGE` as the default image.

### Workflow Steps
1. **resolve**: Pins the requested image (from input or `HUB_IMAGE`) to its digest, ensuring production deploys exactly what nonlive tested. Extracts the repository name and resolves the manifest digest via `docker buildx imagetools`.
2. **nonlive**: Deploys to nonlive using the resolved image digest
3. **production**: Depends on nonlive success; requires environment approvers to promote the same tested image

### Security
Uses GitHub OIDC tokens for cloud login, scoped per environment—nonlive cannot reach production. Hub ownership is defined in `deploy/environments/<env>/settings.yaml`, reviewed as code.

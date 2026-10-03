<!-- dth:generated source="deploy/environments/nonlive/values.yaml" — edit only inside dth:human blocks -->
# `deploy/environments/nonlive/values.yaml`

<!-- dth:chunk f5bff08c91104f59 -->
## `deploy/environments/nonlive/values.yaml`

This file defines Helm chart values for the nonlive environment instance of DocTheRepo hub. It configures the same application image and chart as production but with isolated nonlive-specific resources: a dedicated database secret, KMS encryption key in the nonlive AWS account, a separate OIDC SSO application with its own callback URL, and branded SMTP configuration that identifies emails as coming from the nonlive instance. The `environment: nonlive` setting triggers a visual "Nonlive environment" banner in the UI to help users avoid confusion.

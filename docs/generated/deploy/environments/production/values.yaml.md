<!-- dth:generated source="deploy/environments/production/values.yaml" — edit only inside dth:human blocks -->
# `deploy/environments/production/values.yaml`

<!-- dth:chunk 9485d6a97bbb93ec -->
## `deploy/environments/production/values.yaml`

Helm values configuration for production deployment of a documentation hub. Configures the public-facing HTTPS endpoint, database access via existing Kubernetes secret, AWS KMS-based secret encryption, OIDC authentication through Okta with domain restrictions, SMTP email delivery, and API service with 3 replicas for high availability.

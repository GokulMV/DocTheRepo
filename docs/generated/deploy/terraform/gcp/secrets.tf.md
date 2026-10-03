<!-- dth:generated source="deploy/terraform/gcp/secrets.tf" — edit only inside dth:human blocks -->
# `deploy/terraform/gcp/secrets.tf`

<!-- dth:chunk 53f3c65794f177b5 -->
## `deploy/terraform/gcp/secrets.tf`

Defines GCP Secret Manager resources to store application secrets (database URL, OIDC credentials, owner password, and hub settings) with environment variable mappings. For local authentication mode, generates a 24-character random password for the initial owner account. Creates secrets conditionally based on `auth_mode` and `hub_settings` variables, then grants the hub service account permission to access them. The `secret_envs` local maps secret names to environment variable names (kept non-sensitive for Terraform keys), while `secret_values` holds the actual secret data; each secret is versioned and replicated automatically across GCP regions.

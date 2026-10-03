<!-- dth:generated source="deploy/terraform/aws/kms_secrets.tf" — edit only inside dth:human blocks -->
# `deploy/terraform/aws/kms_secrets.tf`

<!-- dth:chunk c7e9b8eaffd21a61 -->
## `deploy/terraform/aws/kms_secrets.tf`

Defines a customer-managed KMS key (`hub`) for envelope encryption that wraps data keys and encrypts RDS and Secrets Manager entries. Creates a 40-character URL-safe database password for the RDS connection string stored in Secrets Manager. Conditionally generates a 24-character owner password for local authentication mode, an OIDC client secret when OIDC is enabled, and a hub settings file (from `var.hub_settings`) when provided. All secrets are encrypted with the customer-managed KMS key and stored in AWS Secrets Manager with auto-generated name prefixes.

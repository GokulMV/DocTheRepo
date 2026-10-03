<!-- dth:generated source="deploy/terraform/aws/iam.tf" — edit only inside dth:human blocks -->
# `deploy/terraform/aws/iam.tf`

<!-- dth:chunk 216c3512fe5fad21 -->
## `deploy/terraform/aws/iam.tf`

Defines IAM roles and policies for ECS task execution and Hub application runtime. The execution role grants permissions to pull container images, write logs, and access encrypted secrets (database URL, OIDC credentials, hub settings, owner passwords) stored in AWS Secrets Manager and decryption via KMS. The task role permits the Hub application to perform envelope encryption/decryption operations with its own KMS key and optionally assume read-only roles in monitored AWS accounts for cloud resource scanning—deliberately restricted to prevent write operations. Both roles trust the ECS Tasks service principal and use name prefixes for resource uniqueness.

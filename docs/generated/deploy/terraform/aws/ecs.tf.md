<!-- dth:generated source="deploy/terraform/aws/ecs.tf" — edit only inside dth:human blocks -->
# `deploy/terraform/aws/ecs.tf`

<!-- dth:chunk 3415602a198e928a -->
## `deploy/terraform/aws/ecs.tf`

### ECS cluster, task definitions, and services

This file defines AWS ECS infrastructure for running DocTheRepo services across multiple roles (hub, scheduler, etc.) using Fargate.

**ECS Cluster**: Creates a named cluster with Container Insights monitoring enabled.

**Common environment and secrets**: Defines shared environment variables for all tasks (public URL, auth mode, KMS key, log format) and conditionally adds OIDC configuration when `auth_mode == "oidc"`. Secrets are pulled from AWS Secrets Manager: database URL, OIDC client secret (if OIDC enabled), owner password, and hub settings. A lifecycle precondition enforces that OIDC mode requires issuer, client ID, secret, and allowed domains—this is critical because without domain restrictions, any account at the identity provider could become owner on first login.

**Task definitions**: Created per role with Fargate compatibility, combining common and role-specific environment variables. Containers use read-only root filesystem with `/tmp` mounted from a task volume for engine work. Health checks run every 15 seconds. Deployment timeout is 60 seconds to allow graceful shutdown of in-flight jobs.

**ECS services**: One per role, with deployment strategy tuned per role (scheduler uses `max_percent: 100` and `min_healthy: 0` to avoid running two schedulers simultaneously; other roles use blue-green with `max_percent: 200`). Includes circuit breaker rollback. Services with load balancers attached to API target group; health check grace period is 60 seconds. No execute command access is enabled.

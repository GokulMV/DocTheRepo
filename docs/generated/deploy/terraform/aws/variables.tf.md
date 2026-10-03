<!-- dth:generated source="deploy/terraform/aws/variables.tf" — edit only inside dth:human blocks -->
# `deploy/terraform/aws/variables.tf`

<!-- dth:chunk c8013aeae565c704 -->
## `deploy/terraform/aws/variables.tf`

Defines Terraform input variables for deploying the DocTheRepo Hub on AWS. Covers resource naming, networking (VPC, subnets, security groups), public endpoint (domain, certificate, Route53), container image and sizing (API/worker task counts and resources), RDS database configuration, authentication (OIDC or local), cross-account access, and deployment metadata. Most variables have sensible defaults (name prefix "dth", 2 API and 2 worker tasks, db.t4g.medium, 30-day logs); required inputs are region, VPC/subnet IDs, domain name, ACM certificate, container image, and owner email. The auth_mode variable includes validation to enforce "oidc" or "local". Sensitive values like oidc_client_secret and hub_settings are marked as sensitive to avoid exposure in logs. Comments explain key constraints: private subnets must have NAT (tasks need outbound access to git hosts and LLM providers), at least two AZs for availability, push events to one repo serialize but can be burst across repos via worker scaling, and database autoscaling goes up to 5x initial storage.

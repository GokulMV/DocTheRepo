variable "name" {
  description = "Prefix for every resource name."
  type        = string
  default     = "dth"
}

variable "region" {
  description = "AWS region to deploy into."
  type        = string
}

variable "tags" {
  description = "Extra tags applied to every resource."
  type        = map(string)
  default     = {}
}

# --- Network (bring your own VPC; the Hub never needs a NAT-less VPC of its own) ---

variable "vpc_id" {
  description = "VPC to deploy into."
  type        = string
}

variable "public_subnet_ids" {
  description = "Subnets for the ALB (at least two AZs)."
  type        = list(string)
}

variable "private_subnet_ids" {
  description = "Subnets for ECS tasks and RDS (at least two AZs). Tasks need outbound internet (NAT) to reach git hosts and LLM providers."
  type        = list(string)
}

variable "allowed_cidrs" {
  description = "CIDRs allowed to reach the ALB. Webhooks from GitHub/GitLab need their ranges (or 0.0.0.0/0; the Hub verifies signatures)."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

# --- Public endpoint ---

variable "domain_name" {
  description = "Hostname users and webhooks reach, e.g. docs-hub.acme.com."
  type        = string
}

variable "acm_certificate_arn" {
  description = "ACM certificate for domain_name, in this region."
  type        = string
}

variable "route53_zone_id" {
  description = "If set, an alias record for domain_name is created in this hosted zone."
  type        = string
  default     = ""
}

# --- Image and sizing ---

variable "image" {
  description = "Hub container image, e.g. ghcr.io/gokulmv/doctherepo-hub:v1.0.0 (pin a tag or digest)."
  type        = string
}

variable "api_count" {
  description = "API tasks (min 2 for availability)."
  type        = number
  default     = 2
}

variable "worker_count" {
  description = "Worker tasks. Scale up for push bursts across many repos; pushes to one repo stay serialized."
  type        = number
  default     = 2
}

variable "api_cpu" {
  type    = number
  default = 512
}

variable "api_memory" {
  type    = number
  default = 1024
}

variable "worker_cpu" {
  type    = number
  default = 1024
}

variable "worker_memory" {
  type    = number
  default = 2048
}

variable "log_level" {
  type    = string
  default = "info"
}

variable "log_retention_days" {
  type    = number
  default = 30
}

# --- Database ---

variable "db_instance_class" {
  type    = string
  default = "db.t4g.medium"
}

variable "db_allocated_storage" {
  description = "Initial storage in GiB (autoscaling up to 5x)."
  type        = number
  default     = 50
}

variable "db_multi_az" {
  type    = bool
  default = true
}

variable "db_backup_retention_days" {
  type    = number
  default = 14
}

variable "db_deletion_protection" {
  type    = bool
  default = true
}

# --- Auth ---

variable "auth_mode" {
  description = "oidc (Okta/Google/any OIDC IdP) or local (single owner password)."
  type        = string
  default     = "oidc"
  validation {
    condition     = contains(["oidc", "local"], var.auth_mode)
    error_message = "auth_mode must be oidc or local."
  }
}

variable "oidc_issuer" {
  description = "OIDC issuer URL, e.g. https://acme.okta.com or https://accounts.google.com."
  type        = string
  default     = ""
}

variable "oidc_client_id" {
  type    = string
  default = ""
}

variable "oidc_client_secret" {
  description = "Stored in Secrets Manager; never placed in the task definition."
  type        = string
  default     = ""
  sensitive   = true
}

variable "owner_email" {
  description = "First owner account. With auth_mode=local a random password is generated into Secrets Manager; with OIDC this email becomes owner on first sign-in."
  type        = string
}

# --- Cross-account read access (M2 signal sources) ---

variable "readonly_role_arns" {
  description = "Roles created by modules/readonly-roles in watched accounts; the task role may assume them."
  type        = list(string)
  default     = []
}

variable "oidc_allowed_domains" {
  description = "Email domains allowed to sign in with OIDC (required for oidc mode: with Google as IdP, an empty list would admit any Google account, and the first sign-in becomes owner)."
  type        = list(string)
  default     = []
}

variable "hub_settings" {
  description = "Optional settings file (YAML) applied when the Hub starts: sign-in and SSO, users and owners, models, connectors, repositories. Generate one with `dth init`. Stored in the secret manager, never in the task definition."
  type        = string
  default     = ""
  sensitive   = true
}

variable "environment" {
  description = "Name of this deployment, e.g. nonlive or production. Anything but production shows a banner in the UI."
  type        = string
  default     = "production"
}

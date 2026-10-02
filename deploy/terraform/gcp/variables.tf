variable "project_id" {
  type = string
}

variable "region" {
  type = string
}

variable "name" {
  description = "Prefix for every resource name."
  type        = string
  default     = "dth"
}

variable "labels" {
  type    = map(string)
  default = {}
}

# --- Network ---

variable "network" {
  description = "VPC network self link or id. Cloud SQL gets a private IP here; Cloud Run reaches it with direct VPC egress."
  type        = string
}

variable "subnetwork" {
  description = "Subnet (same region) for Cloud Run direct VPC egress."
  type        = string
}

variable "create_private_service_access" {
  description = "Reserve a range and peer servicenetworking for Cloud SQL private IP. Set false if the VPC already has it."
  type        = bool
  default     = true
}

# --- Public endpoint ---

variable "domain_name" {
  description = "Hostname for the Hub, e.g. docs-hub.acme.com. Point an A record at the load balancer IP output; the managed certificate provisions once DNS resolves."
  type        = string
}

# --- Image and sizing ---

variable "image" {
  description = "Hub image in Artifact Registry or Docker Hub (Cloud Run cannot pull from ghcr.io directly: mirror it or use an Artifact Registry remote repository)."
  type        = string
}

variable "api_min_instances" {
  type    = number
  default = 1
}

variable "api_max_instances" {
  type    = number
  default = 10
}

variable "worker_min_instances" {
  type    = number
  default = 1
}

variable "worker_max_instances" {
  type    = number
  default = 5
}

variable "log_level" {
  type    = string
  default = "info"
}

# --- Database ---

variable "db_tier" {
  type    = string
  default = "db-custom-2-7680"
}

variable "db_disk_size_gb" {
  type    = number
  default = 50
}

variable "db_high_availability" {
  type    = bool
  default = true
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
  type    = string
  default = ""
}

variable "oidc_client_id" {
  type    = string
  default = ""
}

variable "oidc_client_secret" {
  type      = string
  default   = ""
  sensitive = true
}

variable "oidc_allowed_domains" {
  description = "Email domains allowed to sign in (required for oidc: with Google as IdP an empty list would admit any Google account, and the first sign-in becomes owner)."
  type        = list(string)
  default     = []
}

variable "owner_email" {
  description = "First owner account (local mode: password generated into Secret Manager)."
  type        = string
}

variable "hub_settings" {
  description = "Optional settings file (YAML) applied when the Hub starts: sign-in and SSO, users and owners, models, connectors, repositories. Generate one with `dth init`. Stored in the secret manager, never in the task definition."
  type        = string
  default     = ""
  sensitive   = true
}

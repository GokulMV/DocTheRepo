<!-- dth:generated source="deploy/terraform/gcp/variables.tf" — edit only inside dth:human blocks -->
# `deploy/terraform/gcp/variables.tf`

<!-- dth:chunk 7f28882f9adf14d2 -->
## `deploy/terraform/gcp/variables.tf`

This file declares all input variables for the Terraform GCP deployment, organizing them into sections: basic infrastructure (project, region, naming, labels), networking (VPC and subnet configuration for Cloud SQL private IP and Cloud Run egress), public endpoint (domain name for managed certificate provisioning), compute sizing (min/max instances for API and worker Cloud Run services, log level), database configuration (Cloud SQL tier, disk size, HA, deletion protection), and authentication (supporting both OIDC with IdP configuration and local password modes, with email domain restrictions and owner setup). The `auth_mode` variable includes validation to restrict values to "oidc" or "local". Sensitive variables like `oidc_client_secret` and `hub_settings` are marked appropriately.

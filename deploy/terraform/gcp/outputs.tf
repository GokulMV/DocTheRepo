output "url" {
  value = local.public_url
}

output "load_balancer_ip" {
  description = "Create an A record for domain_name pointing here; the managed certificate then provisions."
  value       = google_compute_global_address.hub.address
}

output "oidc_redirect_url" {
  description = "Register this redirect URI with your IdP (Okta / Google)."
  value       = "${local.public_url}/api/v1/auth/callback"
}

output "service_account_email" {
  description = "Pass to modules/readonly-roles/gcp as hub_service_account in each watched project."
  value       = google_service_account.hub.email
}

output "kms_key_id" {
  value = google_kms_crypto_key.hub.id
}

output "owner_password_secret" {
  description = "Local auth mode: read the first owner password here, then change it."
  value       = try(google_secret_manager_secret.hub["owner-password"].secret_id, null)
}

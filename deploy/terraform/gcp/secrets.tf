resource "random_password" "owner" {
  count   = var.auth_mode == "local" ? 1 : 0
  length  = 24
  special = false
}

locals {
  # Secret name → env var. Kept apart from the (sensitive) values so for_each keys stay plain.
  secret_envs = merge(
    { "database-url" = "DTH_DATABASE_URL" },
    var.auth_mode == "oidc" ? { "oidc-client-secret" = "DTH_OIDC_CLIENT_SECRET" } : {},
    # Local mode: the owner's first password (used only while the users table is empty).
    var.auth_mode == "local" ? { "owner-password" = "DTH_OWNER_PASSWORD" } : {},
    # Settings applied at start (sign-in, users, models…), generated with `dth init`.
    nonsensitive(var.hub_settings != "") ? { "hub-settings" = "DTH_SETTINGS" } : {},
  )
  secret_values = {
    "database-url"       = "postgres://dth:${random_password.db.result}@${google_sql_database_instance.hub.private_ip_address}:5432/dth?sslmode=require"
    "oidc-client-secret" = var.oidc_client_secret
    "owner-password"     = try(random_password.owner[0].result, "")
    "hub-settings"       = var.hub_settings
  }
}

resource "google_secret_manager_secret" "hub" {
  for_each  = local.secret_envs
  secret_id = "${var.name}-${each.key}"
  labels    = local.labels
  replication {
    auto {}
  }
  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "hub" {
  for_each    = google_secret_manager_secret.hub
  secret      = each.value.id
  secret_data = local.secret_values[each.key]
}

resource "google_secret_manager_secret_iam_member" "hub" {
  for_each  = google_secret_manager_secret.hub
  secret_id = each.value.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.hub.email}"
}

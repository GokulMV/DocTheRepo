resource "google_compute_global_address" "sql_range" {
  count         = var.create_private_service_access ? 1 : 0
  name          = "${var.name}-sql-range"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 20
  network       = var.network
}

resource "google_service_networking_connection" "sql" {
  count                   = var.create_private_service_access ? 1 : 0
  network                 = var.network
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.sql_range[0].name]
  depends_on              = [google_project_service.apis]
}

resource "google_sql_database_instance" "hub" {
  name                = "${var.name}-hub"
  database_version    = "POSTGRES_16"
  region              = var.region
  deletion_protection = var.db_deletion_protection
  settings {
    tier              = var.db_tier
    edition           = "ENTERPRISE"
    availability_type = var.db_high_availability ? "REGIONAL" : "ZONAL"
    disk_size         = var.db_disk_size_gb
    disk_autoresize   = true
    user_labels       = local.labels
    ip_configuration {
      ipv4_enabled    = false
      private_network = var.network
      ssl_mode        = "ENCRYPTED_ONLY"
    }
    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
    }
    insights_config {
      query_insights_enabled = true
    }
  }
  depends_on = [google_service_networking_connection.sql]
}

resource "google_sql_database" "hub" {
  name     = "dth"
  instance = google_sql_database_instance.hub.name
}

# URL-safe characters only so the password can sit in the connection URL without escaping.
resource "random_password" "db" {
  length  = 40
  special = false
}

# pgvector ships with Cloud SQL PostgreSQL 16; the Hub runs CREATE EXTENSION itself (cloudsqlsuperuser may).
resource "google_sql_user" "hub" {
  name     = "dth"
  instance = google_sql_database_instance.hub.name
  password = random_password.db.result
}

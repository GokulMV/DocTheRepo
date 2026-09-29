# DocTheRepo Hub on GCP: HTTPS load balancer (managed cert) → Cloud Run api; Cloud Run worker + scheduler
# (CPU always allocated); Cloud SQL PostgreSQL 16 on a private IP; Secret Manager; Cloud KMS.

locals {
  public_url = "https://${var.domain_name}"
  labels     = merge({ app = "doctherepo-hub", managed-by = "terraform" }, var.labels)
}


resource "google_project_service" "apis" {
  for_each = toset([
    "run.googleapis.com", "sqladmin.googleapis.com", "secretmanager.googleapis.com", "cloudkms.googleapis.com",
    "servicenetworking.googleapis.com", "compute.googleapis.com",
  ])
  service            = each.value
  disable_on_destroy = false
}

# The identity every Hub service runs as. Grant it read roles in watched projects with
# modules/readonly-roles/gcp.
resource "google_service_account" "hub" {
  account_id   = "${var.name}-hub"
  display_name = "DocTheRepo Hub"
}

# --- KMS: wraps the Hub's data keys (connector credentials, LLM keys) ---

resource "google_kms_key_ring" "hub" {
  name       = "${var.name}-hub"
  location   = var.region
  depends_on = [google_project_service.apis]
}

resource "google_kms_crypto_key" "hub" {
  name            = "envelope"
  key_ring        = google_kms_key_ring.hub.id
  rotation_period = "7776000s" # 90 days; old versions stay enabled so existing ciphertexts decrypt
  lifecycle { prevent_destroy = true }
}

resource "google_kms_crypto_key_iam_member" "hub" {
  crypto_key_id = google_kms_crypto_key.hub.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = "serviceAccount:${google_service_account.hub.email}"
}

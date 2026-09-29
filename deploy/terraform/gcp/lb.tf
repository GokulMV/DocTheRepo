resource "google_compute_global_address" "hub" {
  name = "${var.name}-hub"
}

resource "google_compute_region_network_endpoint_group" "api" {
  name                  = "${var.name}-api"
  region                = var.region
  network_endpoint_type = "SERVERLESS"
  cloud_run {
    service = google_cloud_run_v2_service.hub["api"].name
  }
}

resource "google_compute_backend_service" "api" {
  name                  = "${var.name}-api"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  protocol              = "HTTPS"
  backend {
    group = google_compute_region_network_endpoint_group.api.id
  }
  log_config {
    enable = true
  }
}

resource "google_compute_url_map" "hub" {
  name            = "${var.name}-hub"
  default_service = google_compute_backend_service.api.id
}

resource "google_compute_managed_ssl_certificate" "hub" {
  name = "${var.name}-hub"
  managed {
    domains = [var.domain_name]
  }
}

resource "google_compute_ssl_policy" "hub" {
  name            = "${var.name}-hub"
  profile         = "MODERN"
  min_tls_version = "TLS_1_2"
}

resource "google_compute_target_https_proxy" "hub" {
  name             = "${var.name}-hub"
  url_map          = google_compute_url_map.hub.id
  ssl_certificates = [google_compute_managed_ssl_certificate.hub.id]
  ssl_policy       = google_compute_ssl_policy.hub.id
}

resource "google_compute_global_forwarding_rule" "https" {
  name                  = "${var.name}-hub-https"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  target                = google_compute_target_https_proxy.hub.id
  ip_address            = google_compute_global_address.hub.address
  port_range            = "443"
}

# Plain HTTP only redirects to HTTPS.
resource "google_compute_url_map" "redirect" {
  name = "${var.name}-hub-redirect"
  default_url_redirect {
    https_redirect         = true
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
    strip_query            = false
  }
}

resource "google_compute_target_http_proxy" "redirect" {
  name    = "${var.name}-hub-redirect"
  url_map = google_compute_url_map.redirect.id
}

resource "google_compute_global_forwarding_rule" "http" {
  name                  = "${var.name}-hub-http"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  target                = google_compute_target_http_proxy.redirect.id
  ip_address            = google_compute_global_address.hub.address
  port_range            = "80"
}

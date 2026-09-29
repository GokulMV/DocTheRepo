locals {
  common_env = merge(
    {
      DTH_PUBLIC_URL       = local.public_url
      DTH_LISTEN           = "0.0.0.0:8080"
      DTH_METRICS_LISTEN   = "0.0.0.0:9090"
      DTH_SECRETS_PROVIDER = "gcpkms"
      DTH_KMS_KEY_ID       = google_kms_crypto_key.hub.id
      DTH_AUTH_MODE        = var.auth_mode
      DTH_OWNER_EMAIL      = var.owner_email
      DTH_LOG_FORMAT       = "json"
      DTH_LOG_LEVEL        = var.log_level
    },
    var.auth_mode == "oidc" ? {
      DTH_OIDC_ISSUER          = var.oidc_issuer
      DTH_OIDC_CLIENT_ID       = var.oidc_client_id
      DTH_OIDC_REDIRECT_URL    = "${local.public_url}/api/v1/auth/callback"
      DTH_OIDC_ALLOWED_DOMAINS = join(",", var.oidc_allowed_domains)
    } : {},
  )

  # api serves HTTP on 8080 behind the load balancer. worker and scheduler have no API listener; Cloud Run
  # probes their metrics listener (9090, /healthz) instead. CPU stays allocated so jobs, pollers, and the
  # scheduler keep running between requests.
  services = {
    api = {
      port = 8080, probe = "/readyz", min = var.api_min_instances, max = var.api_max_instances,
      cpu  = "1", memory = "1Gi", ingress = "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER", cpu_idle = true
    }
    worker = {
      port = 9090, probe = "/healthz", min = var.worker_min_instances, max = var.worker_max_instances,
      cpu  = "2", memory = "2Gi", ingress = "INGRESS_TRAFFIC_INTERNAL_ONLY", cpu_idle = false
    }
    scheduler = {
      port = 9090, probe = "/healthz", min = 1, max = 1,
      cpu  = "1", memory = "512Mi", ingress = "INGRESS_TRAFFIC_INTERNAL_ONLY", cpu_idle = false
    }
  }
}

resource "google_cloud_run_v2_service" "hub" {
  for_each = local.services

  lifecycle {
    # Hard failure, not a warning: without a domain allow-list, any account at the IdP (any Google
    # account, for Google) could sign in, and the first sign-in becomes owner.
    precondition {
      condition     = var.auth_mode != "oidc" || (var.oidc_issuer != "" && var.oidc_client_id != "" && var.oidc_client_secret != "" && length(var.oidc_allowed_domains) > 0)
      error_message = "auth_mode=oidc needs oidc_issuer, oidc_client_id, oidc_client_secret, and oidc_allowed_domains."
    }
  }
  name                = "${var.name}-${each.key}"
  location            = var.region
  ingress             = each.value.ingress
  deletion_protection = false
  labels              = local.labels

  template {
    service_account                  = google_service_account.hub.email
    timeout                          = "3600s" # Q&A streams answers over SSE
    max_instance_request_concurrency = each.key == "api" ? 80 : 1
    scaling {
      min_instance_count = each.value.min
      max_instance_count = each.value.max
    }
    # Direct VPC egress for Cloud SQL's private IP; internet traffic (git hosts, LLMs) goes out directly.
    vpc_access {
      egress = "PRIVATE_RANGES_ONLY"
      network_interfaces {
        network    = var.network
        subnetwork = var.subnetwork
      }
    }
    containers {
      image = var.image
      ports {
        container_port = each.value.port
      }
      resources {
        limits            = { cpu = each.value.cpu, memory = each.value.memory }
        cpu_idle          = each.value.cpu_idle
        startup_cpu_boost = true
      }
      dynamic "env" {
        for_each = merge(local.common_env, { DTH_ROLES = each.key })
        content {
          name  = env.key
          value = env.value
        }
      }
      dynamic "env" {
        for_each = google_secret_manager_secret.hub
        content {
          name = local.secret_envs[env.key]
          value_source {
            secret_key_ref {
              secret  = env.value.secret_id
              version = "latest"
            }
          }
        }
      }
      startup_probe {
        http_get {
          path = each.value.probe
          port = each.value.port
        }
        initial_delay_seconds = 2
        period_seconds        = 5
        failure_threshold     = 24 # first start runs migrations
      }
      liveness_probe {
        http_get {
          path = each.key == "api" ? "/healthz" : each.value.probe
          port = each.value.port
        }
        period_seconds = 15
      }
    }
  }

  depends_on = [
    google_secret_manager_secret_version.hub,
    google_secret_manager_secret_iam_member.hub,
    google_kms_crypto_key_iam_member.hub,
    google_sql_user.hub,
  ]
}

# The Hub authenticates every request itself (OIDC sessions, PATs, webhook signatures), and ingress is
# limited to the load balancer, so the api service accepts unauthenticated invocations from it.
resource "google_cloud_run_v2_service_iam_member" "api_public" {
  name     = google_cloud_run_v2_service.hub["api"].name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}

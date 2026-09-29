terraform {
  required_version = ">= 1.6"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.10"
    }
  }
}

variable "project_id" {
  description = "The watched project."
  type        = string
}

variable "hub_service_account" {
  description = "The Hub's service account email (output service_account_email of deploy/terraform/gcp)."
  type        = string
}

variable "dead_letter_topics" {
  description = "Dead-letter topic names in this project. Each gets a Hub-owned subscription for sampling."
  type        = list(string)
  default     = []
}

locals {
  member = "serviceAccount:${var.hub_service_account}"
}

resource "google_project_iam_member" "viewer" {
  for_each = toset([
    "roles/logging.viewer", "roles/errorreporting.viewer", "roles/monitoring.viewer", "roles/pubsub.viewer",
  ])
  project = var.project_id
  role    = each.value
  member  = local.member
}

# Hub-owned subscriptions on dead-letter topics: the Hub pulls and acks these only, never an application
# subscription. Messages expire after 7 days so an idle Hub cannot grow them without bound.
resource "google_pubsub_subscription" "dlq" {
  for_each                   = toset(var.dead_letter_topics)
  project                    = var.project_id
  name                       = "dth-hub-${each.value}"
  topic                      = "projects/${var.project_id}/topics/${each.value}"
  ack_deadline_seconds       = 60
  message_retention_duration = "604800s"
  expiration_policy {
    ttl = "" # never expires
  }
}

resource "google_pubsub_subscription_iam_member" "dlq" {
  for_each     = google_pubsub_subscription.dlq
  project      = var.project_id
  subscription = each.value.name
  role         = "roles/pubsub.subscriber"
  member       = local.member
}

output "dlq_subscriptions" {
  value = [for s in google_pubsub_subscription.dlq : s.id]
}

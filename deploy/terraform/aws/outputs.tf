output "url" {
  description = "Hub URL (point webhooks at <url>/hooks/...)."
  value       = local.public_url
}

output "alb_dns_name" {
  description = "Create a CNAME/alias to this if route53_zone_id was not set."
  value       = aws_lb.hub.dns_name
}

output "oidc_redirect_url" {
  description = "Register this redirect URI with your IdP (Okta / Google)."
  value       = "${local.public_url}/api/v1/auth/callback"
}

output "task_role_arn" {
  description = "Pass to modules/readonly-roles as hub_principal_arn in each watched account."
  value       = aws_iam_role.task.arn
}

output "kms_key_arn" {
  value = aws_kms_key.hub.arn
}

output "owner_password_secret_arn" {
  description = "Local auth mode: read the first owner password here, then change it."
  value       = try(aws_secretsmanager_secret.owner_password[0].arn, null)
}

output "log_group" {
  value = aws_cloudwatch_log_group.hub.name
}

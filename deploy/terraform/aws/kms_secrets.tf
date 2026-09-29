# One customer-managed key: wraps the Hub's data keys (connector credentials, LLM keys) and encrypts RDS,
# Secrets Manager entries, and nothing else.
resource "aws_kms_key" "hub" {
  description             = "${var.name} DocTheRepo Hub envelope key"
  enable_key_rotation     = true
  deletion_window_in_days = 30
}

resource "aws_kms_alias" "hub" {
  name          = "alias/${var.name}-hub"
  target_key_id = aws_kms_key.hub.key_id
}

# URL-safe characters only so the password can sit in the connection URL without escaping.
resource "random_password" "db" {
  length  = 40
  special = false
}

resource "random_password" "owner" {
  count   = var.auth_mode == "local" ? 1 : 0
  length  = 24
  special = false
}

resource "aws_secretsmanager_secret" "database_url" {
  name_prefix = "${var.name}/database-url-"
  kms_key_id  = aws_kms_key.hub.arn
}

resource "aws_secretsmanager_secret_version" "database_url" {
  secret_id     = aws_secretsmanager_secret.database_url.id
  secret_string = "postgres://dth:${random_password.db.result}@${aws_db_instance.hub.address}:5432/dth?sslmode=require"
}

resource "aws_secretsmanager_secret" "oidc_client_secret" {
  count       = var.auth_mode == "oidc" ? 1 : 0
  name_prefix = "${var.name}/oidc-client-secret-"
  kms_key_id  = aws_kms_key.hub.arn
}

resource "aws_secretsmanager_secret_version" "oidc_client_secret" {
  count         = var.auth_mode == "oidc" ? 1 : 0
  secret_id     = aws_secretsmanager_secret.oidc_client_secret[0].id
  secret_string = var.oidc_client_secret
}

# Local mode: the owner signs in with this password (read it from Secrets Manager, then change it in the UI).
# It is only used while the users table is empty.
resource "aws_secretsmanager_secret" "owner_password" {
  count       = var.auth_mode == "local" ? 1 : 0
  name_prefix = "${var.name}/owner-password-"
  kms_key_id  = aws_kms_key.hub.arn
}

resource "aws_secretsmanager_secret_version" "owner_password" {
  count         = var.auth_mode == "local" ? 1 : 0
  secret_id     = aws_secretsmanager_secret.owner_password[0].id
  secret_string = random_password.owner[0].result
}

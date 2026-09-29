resource "aws_db_subnet_group" "hub" {
  name_prefix = "${var.name}-"
  subnet_ids  = var.private_subnet_ids
}

# pgvector ships with RDS PostgreSQL 16; the Hub runs CREATE EXTENSION itself (the master user may).
# TLS is enforced (rds.force_ssl) and the URL uses sslmode=require.
resource "aws_db_parameter_group" "hub" {
  name_prefix = "${var.name}-pg16-"
  family      = "postgres16"
  parameter {
    name  = "rds.force_ssl"
    value = "1"
  }
  lifecycle { create_before_destroy = true }
}

resource "aws_db_instance" "hub" {
  identifier_prefix               = "${var.name}-"
  engine                          = "postgres"
  engine_version                  = "16"
  instance_class                  = var.db_instance_class
  allocated_storage               = var.db_allocated_storage
  max_allocated_storage           = var.db_allocated_storage * 5
  storage_type                    = "gp3"
  storage_encrypted               = true
  kms_key_id                      = aws_kms_key.hub.arn
  db_name                         = "dth"
  username                        = "dth"
  password                        = random_password.db.result
  db_subnet_group_name            = aws_db_subnet_group.hub.name
  parameter_group_name            = aws_db_parameter_group.hub.name
  vpc_security_group_ids          = [aws_security_group.db.id]
  multi_az                        = var.db_multi_az
  backup_retention_period         = var.db_backup_retention_days
  deletion_protection             = var.db_deletion_protection
  skip_final_snapshot             = !var.db_deletion_protection
  final_snapshot_identifier       = var.db_deletion_protection ? "${var.name}-hub-final" : null
  auto_minor_version_upgrade      = true
  performance_insights_enabled    = true
  performance_insights_kms_key_id = aws_kms_key.hub.arn
  copy_tags_to_snapshot           = true
}

# DocTheRepo Hub on AWS: ALB (ACM TLS) → ECS Fargate api / worker / scheduler → RDS PostgreSQL 16 (pgvector).
# Secrets live in Secrets Manager; connector credentials and LLM keys are envelope-encrypted with a KMS key.

locals {
  public_url = "https://${var.domain_name}"
  # Every role runs the same image; DTH_ROLES selects what the process does.
  roles = {
    api = {
      count = var.api_count, cpu = var.api_cpu, memory = var.api_memory, lb = true
    }
    worker = {
      count = var.worker_count, cpu = var.worker_cpu, memory = var.worker_memory, lb = false
    }
    # Exactly one scheduler: it owns polling, sweeps, and rollups (it takes a leader lock anyway).
    scheduler = {
      count = 1, cpu = 256, memory = 512, lb = false
    }
  }
}


# --- Security groups: internet → ALB → tasks → RDS ---

resource "aws_security_group" "alb" {
  name_prefix = "${var.name}-alb-"
  description = "DocTheRepo Hub load balancer"
  vpc_id      = var.vpc_id
  lifecycle { create_before_destroy = true }
}

resource "aws_vpc_security_group_ingress_rule" "alb_https" {
  for_each          = toset(var.allowed_cidrs)
  security_group_id = aws_security_group.alb.id
  cidr_ipv4         = each.value
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "alb_http" {
  for_each          = toset(var.allowed_cidrs)
  security_group_id = aws_security_group.alb.id
  cidr_ipv4         = each.value
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "alb_to_tasks" {
  security_group_id            = aws_security_group.alb.id
  referenced_security_group_id = aws_security_group.tasks.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

resource "aws_security_group" "tasks" {
  name_prefix = "${var.name}-tasks-"
  description = "DocTheRepo Hub tasks"
  vpc_id      = var.vpc_id
  lifecycle { create_before_destroy = true }
}

resource "aws_vpc_security_group_ingress_rule" "tasks_from_alb" {
  security_group_id            = aws_security_group.tasks.id
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

# Tasks call git hosts, LLM providers, and cloud APIs over HTTPS, and Postgres inside the VPC.
resource "aws_vpc_security_group_egress_rule" "tasks_all" {
  security_group_id = aws_security_group.tasks.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "db" {
  name_prefix = "${var.name}-db-"
  description = "DocTheRepo Hub database"
  vpc_id      = var.vpc_id
  lifecycle { create_before_destroy = true }
}

resource "aws_vpc_security_group_ingress_rule" "db_from_tasks" {
  security_group_id            = aws_security_group.db.id
  referenced_security_group_id = aws_security_group.tasks.id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"
}

resource "aws_cloudwatch_log_group" "hub" {
  name              = "/ecs/${var.name}-hub"
  retention_in_days = var.log_retention_days
}

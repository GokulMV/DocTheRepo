resource "aws_ecs_cluster" "hub" {
  name = "${var.name}-hub"
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

locals {
  common_env = merge(
    {
      DTH_PUBLIC_URL       = local.public_url
      DTH_LISTEN           = "0.0.0.0:8080"
      DTH_METRICS_LISTEN   = "0.0.0.0:9090"
      DTH_SECRETS_PROVIDER = "awskms"
      DTH_KMS_KEY_ID       = aws_kms_key.hub.arn
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
  common_secrets = concat(
    [{ name = "DTH_DATABASE_URL", valueFrom = aws_secretsmanager_secret.database_url.arn }],
    [for s in aws_secretsmanager_secret.oidc_client_secret : { name = "DTH_OIDC_CLIENT_SECRET", valueFrom = s.arn }],
    [for s in aws_secretsmanager_secret.owner_password : { name = "DTH_OWNER_PASSWORD", valueFrom = s.arn }],
  )
}

resource "aws_ecs_task_definition" "role" {
  for_each = local.roles

  lifecycle {
    # Hard failure, not a warning: without a domain allow-list, any account at the IdP (any Google
    # account, for Google) could sign in, and the first sign-in becomes owner.
    precondition {
      condition     = var.auth_mode != "oidc" || (var.oidc_issuer != "" && var.oidc_client_id != "" && var.oidc_client_secret != "" && length(var.oidc_allowed_domains) > 0)
      error_message = "auth_mode=oidc needs oidc_issuer, oidc_client_id, oidc_client_secret, and oidc_allowed_domains."
    }
  }
  family                   = "${var.name}-${each.key}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = each.value.cpu
  memory                   = each.value.memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn
  volume {
    name = "tmp"
  }
  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }
  container_definitions = jsonencode([{
    name         = "hub"
    image        = var.image
    essential    = true
    environment  = [for k, v in merge(local.common_env, { DTH_ROLES = each.key }) : { name = k, value = v }]
    secrets      = local.common_secrets
    portMappings = each.value.lb ? [{ containerPort = 8080, protocol = "tcp" }] : []
    healthCheck = {
      command     = ["CMD", "/dth-hub", "healthcheck"]
      interval    = 15
      timeout     = 5
      retries     = 3
      startPeriod = 30
    }
    # Root filesystem is read-only; scratch space (engine work dirs, temp clones) goes to a task volume.
    readonlyRootFilesystem = true
    mountPoints            = [{ sourceVolume = "tmp", containerPath = "/tmp", readOnly = false }]
    stopTimeout            = 60 # lets in-flight jobs finish (server.shutdown_grace); leases cover the rest
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.hub.name
        awslogs-region        = var.region
        awslogs-stream-prefix = each.key
      }
    }
  }])
}

resource "aws_ecs_service" "role" {
  for_each               = local.roles
  name                   = each.key
  cluster                = aws_ecs_cluster.hub.id
  task_definition        = aws_ecs_task_definition.role[each.key].arn
  desired_count          = each.value.count
  launch_type            = "FARGATE"
  enable_execute_command = false
  propagate_tags         = "SERVICE"
  # The scheduler must never run twice for long: stop the old task before starting the new one.
  deployment_maximum_percent         = each.key == "scheduler" ? 100 : 200
  deployment_minimum_healthy_percent = each.key == "scheduler" ? 0 : 100
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }
  network_configuration {
    subnets          = var.private_subnet_ids
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = false
  }
  dynamic "load_balancer" {
    for_each = each.value.lb ? [1] : []
    content {
      target_group_arn = aws_lb_target_group.api.arn
      container_name   = "hub"
      container_port   = 8080
    }
  }
  health_check_grace_period_seconds = each.value.lb ? 60 : null
  depends_on                        = [aws_lb_listener.https]
}

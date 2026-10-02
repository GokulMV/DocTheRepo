data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

# Execution role: pull the image, write logs, inject secrets into the container at start.
resource "aws_iam_role" "execution" {
  name_prefix        = "${var.name}-exec-"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "execution_base" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

data "aws_iam_policy_document" "execution_secrets" {
  statement {
    actions = ["secretsmanager:GetSecretValue"]
    resources = concat(
      [aws_secretsmanager_secret.database_url.arn],
      aws_secretsmanager_secret.oidc_client_secret[*].arn,
      aws_secretsmanager_secret.hub_settings[*].arn,
      aws_secretsmanager_secret.owner_password[*].arn,
    )
  }
  statement {
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.hub.arn]
  }
}

resource "aws_iam_role_policy" "execution_secrets" {
  role   = aws_iam_role.execution.id
  policy = data.aws_iam_policy_document.execution_secrets.json
}

# Task role: what the Hub process itself may do. Envelope encryption with the Hub key, and (optionally)
# assuming the read-only roles in watched accounts. Nothing else: the Hub never writes to your cloud.
resource "aws_iam_role" "task" {
  name_prefix        = "${var.name}-task-"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

data "aws_iam_policy_document" "task" {
  statement {
    actions   = ["kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey", "kms:DescribeKey"]
    resources = [aws_kms_key.hub.arn]
  }
  dynamic "statement" {
    for_each = length(var.readonly_role_arns) > 0 ? [1] : []
    content {
      actions   = ["sts:AssumeRole"]
      resources = var.readonly_role_arns
    }
  }
}

resource "aws_iam_role_policy" "task" {
  role   = aws_iam_role.task.id
  policy = data.aws_iam_policy_document.task.json
}

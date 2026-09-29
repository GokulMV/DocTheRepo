terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.70"
    }
  }
}

variable "name" {
  type    = string
  default = "dth-hub-readonly"
}

variable "hub_principal_arn" {
  description = "The Hub's ECS task role ARN (output task_role_arn of deploy/terraform/aws)."
  type        = string
}

variable "external_id" {
  description = "Shared secret the Hub presents on AssumeRole (enter the same value on the Hub's AWS connector)."
  type        = string
  sensitive   = true
}

variable "enable_sqs_peek" {
  description = "Allow ReceiveMessage on DLQs for message samples (visibility timeout 0, never deletes). Off by default: a receive increments ApproximateReceiveCount."
  type        = bool
  default     = false
}

variable "sqs_dlq_arns" {
  description = "DLQs the Hub may peek when enable_sqs_peek is true."
  type        = list(string)
  default     = []
}

variable "msk_cluster_arns" {
  description = "MSK clusters (IAM auth) whose consumer-group lag and DLQ topics the Hub inspects."
  type        = list(string)
  default     = []
}

variable "msk_dlq_topic_patterns" {
  description = "Topic name patterns the Hub may read with its own consumer group."
  type        = list(string)
  default     = ["*.dlq", "*-dlq", "*.DLT"]
}

data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "AWS"
      identifiers = [var.hub_principal_arn]
    }
    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [var.external_id]
    }
  }
}

resource "aws_iam_role" "hub" {
  name                 = var.name
  assume_role_policy   = data.aws_iam_policy_document.trust.json
  max_session_duration = 3600
}

data "aws_iam_policy_document" "read" {
  statement {
    sid = "Logs"
    actions = [
      "logs:DescribeLogGroups", "logs:DescribeLogStreams", "logs:FilterLogEvents", "logs:GetLogEvents",
      "logs:StartQuery", "logs:GetQueryResults", "logs:StopQuery", "logs:DescribeSubscriptionFilters",
    ]
    resources = ["*"]
  }
  statement {
    sid = "MetricsAndAlarms"
    actions = [
      "cloudwatch:DescribeAlarms", "cloudwatch:DescribeAlarmHistory", "cloudwatch:GetMetricData",
      "cloudwatch:ListMetrics",
    ]
    resources = ["*"]
  }
  statement {
    sid = "EventBuses"
    actions = [
      "sqs:ListQueues", "sqs:GetQueueAttributes", "sqs:ListDeadLetterSourceQueues", "sqs:ListQueueTags",
      "sns:ListTopics", "sns:GetTopicAttributes", "sns:ListSubscriptions", "sns:GetSubscriptionAttributes",
      "events:ListEventBuses", "events:ListRules", "events:DescribeRule", "events:ListTargetsByRule",
      "kinesis:ListStreams", "kinesis:DescribeStreamSummary", "kinesis:ListStreamConsumers",
      "lambda:ListEventSourceMappings", "lambda:GetFunctionEventInvokeConfig",
      "kafka:ListClustersV2", "kafka:DescribeClusterV2", "kafka:GetBootstrapBrokers",
    ]
    resources = ["*"]
  }
  dynamic "statement" {
    for_each = var.enable_sqs_peek && length(var.sqs_dlq_arns) > 0 ? [1] : []
    content {
      sid       = "SqsDlqPeek"
      actions   = ["sqs:ReceiveMessage"]
      resources = var.sqs_dlq_arns
    }
  }
  dynamic "statement" {
    for_each = length(var.msk_cluster_arns) > 0 ? [1] : []
    content {
      sid       = "MskDescribe"
      actions   = ["kafka-cluster:Connect", "kafka-cluster:DescribeCluster", "kafka-cluster:DescribeGroup", "kafka-cluster:DescribeTopic"]
      resources = concat(var.msk_cluster_arns, [for c in var.msk_cluster_arns : "${replace(c, ":cluster/", ":group/")}/*"], [for c in var.msk_cluster_arns : "${replace(c, ":cluster/", ":topic/")}/*"])
    }
  }
  dynamic "statement" {
    for_each = length(var.msk_cluster_arns) > 0 ? [1] : []
    content {
      sid       = "MskReadDlqTopics"
      actions   = ["kafka-cluster:ReadData"]
      resources = flatten([for c in var.msk_cluster_arns : [for p in var.msk_dlq_topic_patterns : "${replace(c, ":cluster/", ":topic/")}/${p}"]])
    }
  }
  dynamic "statement" {
    # The Hub's own consumer group only: it commits its own offsets, never an application group's.
    for_each = length(var.msk_cluster_arns) > 0 ? [1] : []
    content {
      sid       = "MskOwnGroup"
      actions   = ["kafka-cluster:AlterGroup"]
      resources = [for c in var.msk_cluster_arns : "${replace(c, ":cluster/", ":group/")}/dth-hub-*"]
    }
  }
}

resource "aws_iam_role_policy" "read" {
  role   = aws_iam_role.hub.id
  policy = data.aws_iam_policy_document.read.json
}

output "role_arn" {
  description = "Enter on the Hub's AWS connector (with the external ID); add to readonly_role_arns in deploy/terraform/aws."
  value       = aws_iam_role.hub.arn
}

# Account-wide budget and application shutdown guardrail.
#
# This is not a hard spending cap: AWS cost data normally arrives hours late,
# and stopped resources retain storage and networking charges. At 100% of the
# actual monthly budget, however, the expensive application compute is latched
# off until an operator explicitly clears the latch and starts it again.

data "aws_partition" "current" {}

resource "aws_sns_topic" "budget_shutdown" {
  name = "${local.name}-budget-shutdown"
}

data "aws_iam_policy_document" "budget_shutdown_topic" {
  statement {
    sid       = "AllowAWSBudgetsToPublish"
    effect    = "Allow"
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.budget_shutdown.arn]

    principals {
      type        = "Service"
      identifiers = ["budgets.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:${data.aws_partition.current.partition}:budgets::${data.aws_caller_identity.current.account_id}:*"]
    }
  }
}

resource "aws_sns_topic_policy" "budget_shutdown" {
  arn    = aws_sns_topic.budget_shutdown.arn
  policy = data.aws_iam_policy_document.budget_shutdown_topic.json
}

resource "aws_budgets_budget" "account_monthly" {
  name         = "${local.name}-account-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_budget_limit)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_alert_email]
    subscriber_sns_topic_arns  = [aws_sns_topic.budget_shutdown.arn]
  }

  depends_on = [aws_sns_topic_policy.budget_shutdown]
}

resource "aws_ssm_parameter" "budget_shutdown_latch" {
  name        = "/${local.name}/budget-shutdown"
  description = "True after the monthly budget shutdown fires; clear manually to resume."
  type        = "String"
  value       = "false"

  lifecycle {
    # Lambda owns the value after creation. Terraform must not silently clear a
    # tripped shutdown latch during an unrelated infrastructure deployment.
    ignore_changes = [value]
  }
}

resource "aws_iam_role" "budget_shutdown" {
  name = "${local.name}-budget-shutdown"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "budget_shutdown_logs" {
  role       = aws_iam_role.budget_shutdown.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "budget_shutdown" {
  name = "${local.name}-budget-shutdown"
  role = aws_iam_role.budget_shutdown.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = ["ecs:UpdateService", "ecs:DescribeServices"]
        Resource = [
          "arn:${data.aws_partition.current.partition}:ecs:${var.region}:${data.aws_caller_identity.current.account_id}:service/${aws_ecs_cluster.main.name}/*"
        ]
      },
      {
        Effect   = "Allow"
        Action   = ["ec2:StopInstances"]
        Resource = [aws_instance.neo4j.arn]
      },
      {
        Effect   = "Allow"
        Action   = ["rds:DescribeDBInstances"]
        Resource = ["*"]
      },
      {
        Effect   = "Allow"
        Action   = ["rds:StopDBInstance"]
        Resource = [aws_db_instance.main.arn]
      },
      {
        Effect   = "Allow"
        Action   = ["ssm:GetParameter", "ssm:PutParameter"]
        Resource = [aws_ssm_parameter.budget_shutdown_latch.arn]
      }
    ]
  })
}

data "archive_file" "budget_shutdown" {
  type        = "zip"
  source_file = "${path.module}/lambda/budget_shutdown.py"
  output_path = "${path.module}/.terraform/budget_shutdown.zip"
}

resource "aws_lambda_function" "budget_shutdown" {
  function_name    = "${local.name}-budget-shutdown"
  role             = aws_iam_role.budget_shutdown.arn
  runtime          = "python3.13"
  handler          = "budget_shutdown.handler"
  filename         = data.archive_file.budget_shutdown.output_path
  source_code_hash = data.archive_file.budget_shutdown.output_base64sha256
  timeout          = 60

  environment {
    variables = {
      ECS_CLUSTER      = aws_ecs_cluster.main.name
      ECS_SERVICES     = join(",", [aws_ecs_service.api.name, aws_ecs_service.worker.name])
      EC2_INSTANCE_IDS = aws_instance.neo4j.id
      RDS_INSTANCE_IDS = aws_db_instance.main.identifier
      LATCH_PARAMETER  = aws_ssm_parameter.budget_shutdown_latch.name
    }
  }
}

resource "aws_lambda_permission" "budget_shutdown_sns" {
  statement_id  = "AllowBudgetSns"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.budget_shutdown.function_name
  principal     = "sns.amazonaws.com"
  source_arn    = aws_sns_topic.budget_shutdown.arn
}

resource "aws_sns_topic_subscription" "budget_shutdown" {
  topic_arn = aws_sns_topic.budget_shutdown.arn
  protocol  = "lambda"
  endpoint  = aws_lambda_function.budget_shutdown.arn

  depends_on = [aws_lambda_permission.budget_shutdown_sns]
}

# RDS starts itself after seven stopped days. Reapplying the latch every six
# hours makes the shutdown persistent without deleting data or infrastructure.
resource "aws_cloudwatch_event_rule" "budget_shutdown_enforcer" {
  name                = "${local.name}-budget-shutdown-enforcer"
  schedule_expression = "rate(6 hours)"
}

resource "aws_cloudwatch_event_target" "budget_shutdown_enforcer" {
  rule = aws_cloudwatch_event_rule.budget_shutdown_enforcer.name
  arn  = aws_lambda_function.budget_shutdown.arn
}

resource "aws_lambda_permission" "budget_shutdown_eventbridge" {
  statement_id  = "AllowBudgetShutdownSchedule"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.budget_shutdown.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.budget_shutdown_enforcer.arn
}

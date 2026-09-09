# Defence in depth on the auth endpoints.
#
# The app throttles /auth/login, /auth/redeem and /admin/challenge itself, in
# process. This adds a layer that sheds load at the edge, before a request
# reaches a task at all -- which is the part the in-process limiter cannot do,
# since by the time it runs the task has already paid for the connection.

resource "aws_wafv2_web_acl" "main" {
  name  = local.name
  scope = "REGIONAL"

  default_action {
    allow {}
  }

  rule {
    name     = "auth-rate-limit"
    priority = 1

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = var.auth_rate_limit_per_5min
        aggregate_key_type = "IP"

        # Scoped down so the limit applies only to the credential endpoints. A
        # blanket rate limit would also throttle a reader paging the corpus,
        # which is exactly the traffic this deployment exists to serve.
        scope_down_statement {
          byte_match_statement {
            positional_constraint = "STARTS_WITH"
            search_string         = "/auth/"
            field_to_match {
              uri_path {}
            }
            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-auth-rate-limit"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = local.name
    sampled_requests_enabled   = true
  }
}

resource "aws_wafv2_web_acl_association" "main" {
  resource_arn = aws_lb.main.arn
  web_acl_arn  = aws_wafv2_web_acl.main.arn
}

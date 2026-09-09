# DNS and TLS.
#
# Your domain is registered elsewhere, so Terraform creates the zone and you
# delegate to it. That makes the first apply two-stage -- see the README: the
# certificate cannot validate until the registrar points at these name servers,
# and `aws_acm_certificate_validation` will sit and wait until it does.

resource "aws_route53_zone" "main" {
  name    = var.hosted_zone_name
  comment = "Managed by Terraform for ${local.name}"
}

resource "aws_acm_certificate" "main" {
  domain_name       = var.domain_name
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_route53_record" "cert_validation" {
  for_each = {
    for option in aws_acm_certificate.main.domain_validation_options :
    option.domain_name => {
      name   = option.resource_record_name
      record = option.resource_record_value
      type   = option.resource_record_type
    }
  }

  zone_id         = aws_route53_zone.main.zone_id
  name            = each.value.name
  type            = each.value.type
  records         = [each.value.record]
  ttl             = 60
  allow_overwrite = true
}

resource "aws_acm_certificate_validation" "main" {
  certificate_arn         = aws_acm_certificate.main.arn
  validation_record_fqdns = [for record in aws_route53_record.cert_validation : record.fqdn]
}

# --- load balancer ---------------------------------------------------------

resource "aws_lb" "main" {
  name               = local.name
  load_balancer_type = "application"
  subnets            = aws_subnet.public[*].id
  security_groups    = [aws_security_group.alb.id]

  # Long enough for the heaviest route: /corpus/{id}/references fetches full
  # papers from PubTator and is deliberately slow.
  idle_timeout = 120

  enable_deletion_protection = !var.destroy_friendly
}

resource "aws_lb_target_group" "api" {
  name        = "${local.name}-api"
  port        = 8000
  protocol    = "HTTP"
  vpc_id      = aws_vpc.main.id
  target_type = "ip" # awsvpc networking: tasks register by ENI address

  health_check {
    path     = "/health"
    matcher  = "200"
    interval = 30
    timeout  = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  # /health, not /health/ready, and the difference matters. Readiness checks
  # Postgres; if the database went down, every task would fail at once, the
  # whole target group would drain, and visitors would get the balancer's 503
  # instead of the app's error -- turning a degraded dependency into a total
  # outage that hides its own cause.

  # Sessions are bearer tokens, not server-side state, so any task can serve
  # any request and stickiness would only concentrate load.
  deregistration_delay = 30
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.main.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate_validation.main.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }
}

# Plain HTTP is redirected, never served. A Secure cookie is not sent over
# http://, so an app reachable both ways looks broken rather than insecure.
resource "aws_lb_listener" "http_redirect" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_route53_record" "app" {
  zone_id = aws_route53_zone.main.zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_lb.main.dns_name
    zone_id                = aws_lb.main.zone_id
    evaluate_target_health = true
  }
}

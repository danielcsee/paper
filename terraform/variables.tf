variable "project" {
  type        = string
  default     = "sciterm"
  description = "Name prefix and Name tag for every resource."
}

variable "region" {
  type        = string
  default     = "us-east-1"
  description = "Deployment region. Cheapest for the services used here."
}

variable "domain_name" {
  type        = string
  description = <<-EOT
    The hostname the app is served on, e.g. "sciterm.example.com". Required:
    the refresh cookie is Secure, so sessions silently fail to survive a reload
    without HTTPS on a real name.
  EOT
}

variable "cloudflare_zone_id" {
  type        = string
  description = "Cloudflare Zone ID for the domain containing domain_name. This identifier is not an API credential."

  validation {
    condition     = can(regex("^[0-9a-f]{32}$", var.cloudflare_zone_id))
    error_message = "cloudflare_zone_id must be the 32-character hexadecimal Zone ID shown in Cloudflare."
  }
}

variable "image_tag" {
  type        = string
  description = "Image tag to deploy. Use a commit SHA, never \"latest\" -- a rollback should be a task-definition revision, not a rebuild."
}

# --- sizing ---------------------------------------------------------------

variable "api_cpu" {
  type        = number
  default     = 512
  description = "Fargate CPU units for the API (512 = 0.5 vCPU)."
}

variable "api_memory" {
  type    = number
  default = 1024
}

variable "worker_cpu" {
  type        = number
  default     = 1024
  description = "The worker holds the embedding model resident, so it is sized above the API."
}

variable "worker_memory" {
  type    = number
  default = 2048
}

variable "api_desired_count" {
  type    = number
  default = 1
}

variable "db_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "db_allocated_storage" {
  type    = number
  default = 20
}

variable "cache_node_type" {
  type    = string
  default = "cache.t4g.micro"
}

variable "neo4j_instance_type" {
  type    = string
  default = "t4g.small"
}

variable "neo4j_root_volume_gb" {
  type        = number
  default     = 30
  description = <<-EOT
    Neo4j lives on the root volume, so replacing the instance loses the graph.
    That is acceptable here: the graph is rebuildable from Postgres with
    scripts/build-graph.sh, which makes it a cache rather than a source of
    truth. Give it its own EBS volume if that ever stops being true.
  EOT
}

# --- lifecycle ------------------------------------------------------------

variable "destroy_friendly" {
  type        = bool
  default     = true
  description = <<-EOT
    Skips the RDS final snapshot and leaves deletion protection off, so
    `terraform destroy` completes without manual steps. Right for a demo you
    apply before interviews and destroy after; set false the moment the
    database holds anything you would miss.
  EOT
}

variable "log_retention_days" {
  type        = number
  default     = 14
  description = "CloudWatch Logs retention. The default of \"never expire\" quietly accrues cost."
}

variable "auth_rate_limit_per_5min" {
  type        = number
  default     = 300
  description = <<-EOT
    WAF rate limit per source IP on /auth/*. Defence in depth: the app throttles
    these endpoints itself, and this sheds load before it reaches a task.
    AWS enforces a floor of 100.
  EOT
}

# --- cost guardrail -------------------------------------------------------

variable "monthly_budget_limit" {
  type        = number
  default     = 200
  description = "Account-wide monthly USD budget. Crossing 100% latches the application off."

  validation {
    condition     = var.monthly_budget_limit > 0
    error_message = "monthly_budget_limit must be greater than zero."
  }
}

variable "budget_alert_email" {
  type        = string
  description = "Email address for warnings and the automatic shutdown notification."

  validation {
    condition     = can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", var.budget_alert_email))
    error_message = "budget_alert_email must be a valid email address."
  }
}

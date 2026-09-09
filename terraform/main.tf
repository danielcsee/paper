provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project   = var.project
      ManagedBy = "terraform"
    }
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_caller_identity" "current" {}

locals {
  name = var.project

  # Two AZs, the minimum an ALB and an RDS subnet group will accept.
  azs = slice(data.aws_availability_zones.available.names, 0, 2)

  # One place that knows how the app is configured, so the API and worker task
  # definitions cannot drift apart on anything they share.
  common_env = [
    { name = "SCITERM_ENV", value = "prod" },
    { name = "SCITERM_VERSION", value = var.image_tag },
    { name = "CELERY_BROKER_URL", value = local.redis_url },
    { name = "CELERY_RESULT_BACKEND", value = "redis://${local.redis_host}:6379/1" },
    { name = "RATE_LIMIT_REDIS_URL", value = local.redis_url },

    # Off deliberately. The cache is a politeness optimisation for PubTator,
    # and its eviction policy cannot share a node with the broker without
    # letting cached documents evict queued tasks. Imports are gated behind
    # access codes, so the volume does not justify a second node yet.
    { name = "DOCUMENT_CACHE_ENABLED", value = "false" },

    # Fargate has no GPU, and the worker's default device selection is only
    # correct on a developer laptop.
    { name = "EMBEDDING_DEVICE", value = "cpu" },
  ]

  redis_host = aws_elasticache_cluster.broker.cache_nodes[0].address

  # Database 0 is the queue, 1 the result backend, matching .env.example. Both
  # live on one node: cluster-mode-disabled Redis has 16 databases, and they
  # share an eviction policy, which is exactly why the *document cache* cannot
  # join them.
  redis_url = "redis://${local.redis_host}:6379/0"
}

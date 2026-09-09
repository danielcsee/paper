resource "aws_elasticache_subnet_group" "main" {
  name       = local.name
  subnet_ids = aws_subnet.private[*].id
}

# One node, for the Celery broker only.
#
# The document cache is deliberately not deployed. Its eviction policy
# (allkeys-lru) cannot share an instance with the broker, because maxmemory
# policy is per-instance and cached documents would then be able to evict
# queued tasks. A second node is a real cost for an optimisation whose value
# scales with import volume -- and imports are gated behind access codes. Add
# it, with its own parameter group, if NCBI's rate limit starts to bite.
resource "aws_elasticache_cluster" "broker" {
  cluster_id      = "${local.name}-broker"
  engine          = "redis"
  engine_version  = "7.1"
  node_type       = var.cache_node_type
  num_cache_nodes = 1

  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.cache.id]
  parameter_group_name = aws_elasticache_parameter_group.broker.name

  # No transit encryption or auth token: single-node clusters use the simpler
  # resource, and the node is unreachable outside its security group. Moving to
  # a replication group would buy TLS at the cost of rediss:// URLs everywhere.
  apply_immediately = true
}

resource "aws_elasticache_parameter_group" "broker" {
  name   = "${local.name}-broker"
  family = "redis7"

  # noeviction, explicitly. The broker must never silently drop a queued task
  # to make room -- that failure looks like a bug in the pipeline.
  parameter {
    name  = "maxmemory-policy"
    value = "noeviction"
  }
}

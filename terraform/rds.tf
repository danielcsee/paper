resource "aws_db_subnet_group" "main" {
  name       = local.name
  subnet_ids = aws_subnet.private[*].id
}

# pgvector is an available extension on Postgres 16; the first migration runs
# `CREATE EXTENSION IF NOT EXISTS vector`, which the master user has the
# rds_superuser role to do. No parameter group change is needed for that, but
# the extension must be allowed to load -- hence shared_preload_libraries is
# left alone: pgvector does not require it.
resource "aws_db_parameter_group" "main" {
  name   = local.name
  family = "postgres16"

  # Log slow queries. The RAG path is the one that will surprise you as the
  # corpus grows, and an unindexed sequential scan is invisible otherwise.
  parameter {
    name  = "log_min_duration_statement"
    value = "1000"
  }
}

resource "aws_db_instance" "main" {
  identifier     = local.name
  engine         = "postgres"
  engine_version = "16"
  instance_class = var.db_instance_class

  db_name  = "sciterm"
  username = "sciterm"
  password = random_password.db.result

  allocated_storage     = var.db_allocated_storage
  max_allocated_storage = var.db_allocated_storage * 5 # storage autoscaling
  storage_type          = "gp3"
  storage_encrypted     = true

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  parameter_group_name   = aws_db_parameter_group.main.name
  publicly_accessible    = false

  multi_az                = false # single-AZ: this is a demo, not an SLA
  backup_retention_period = var.destroy_friendly ? 1 : 7
  skip_final_snapshot     = var.destroy_friendly
  final_snapshot_identifier = var.destroy_friendly ? null : "${local.name}-final-${formatdate("YYYYMMDDhhmmss", timestamp())}"
  deletion_protection     = !var.destroy_friendly

  auto_minor_version_upgrade = true
  apply_immediately          = true

  lifecycle {
    # timestamp() in the snapshot name would otherwise propose a replacement on
    # every single plan.
    ignore_changes = [final_snapshot_identifier]
  }
}

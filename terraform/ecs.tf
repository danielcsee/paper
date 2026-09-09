resource "aws_ecs_cluster" "main" {
  name = local.name

  setting {
    name  = "containerInsights"
    value = "disabled" # per-metric charges, and CloudWatch Logs answers most questions
  }
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/ecs/${local.name}-api"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "worker" {
  name              = "/ecs/${local.name}-worker"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "migrate" {
  name              = "/ecs/${local.name}-migrate"
  retention_in_days = var.log_retention_days
}

locals {
  image = "${aws_ecr_repository.app.repository_url}:${var.image_tag}"

  # ARM64 throughout: you build on Apple silicon, and Graviton Fargate is
  # cheaper per vCPU. It also sidesteps torch pulling the x86 CUDA packages.
  runtime = {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  db_secret = { name = "DATABASE_URL", valueFrom = aws_secretsmanager_secret.database_url.arn }
}

# --- API -------------------------------------------------------------------

resource "aws_ecs_task_definition" "api" {
  family                   = "${local.name}-api"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.api_cpu
  memory                   = var.api_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.api_task.arn

  runtime_platform {
    operating_system_family = local.runtime.operating_system_family
    cpu_architecture        = local.runtime.cpu_architecture
  }

  container_definitions = jsonencode([{
    name      = "api"
    image     = local.image
    essential = true

    # --proxy-headers matters twice over: without it every request logs the
    # load balancer as the client, and the auth throttle's per-address key
    # collapses into a single global bucket.
    command = [
      "uvicorn", "api.app.main:app",
      "--host", "0.0.0.0", "--port", "8000",
      "--proxy-headers", "--forwarded-allow-ips=*",
    ]

    portMappings = [{ containerPort = 8000, protocol = "tcp" }]

    environment = concat(local.common_env, [
      # The API never opens a Neo4j driver, so it is not given a route to one.
    ])

    secrets = [
      local.db_secret,
      # The API is the only task that signs tokens. The worker is deliberately
      # not given this -- see api/auth/config.py.
      { name = "JWT_SECRET", valueFrom = aws_secretsmanager_secret.jwt.arn },
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.api.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "api"
      }
    }
  }])
}

resource "aws_ecs_service" "api" {
  name            = "${local.name}-api"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.api.arn
  desired_count   = var.api_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.api.id]
    assign_public_ip = false # private subnets; egress is via NAT
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "api"
    container_port   = 8000
  }

  # The image bundles the embedding model's dependencies, so a cold start is
  # slower than a bare FastAPI app's. Too short a grace period kills a task
  # that was only still booting.
  health_check_grace_period_seconds = 60

  # Roll forward without dropping to zero capacity on a single-task service.
  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200

  # The listener, so the target group is attached before tasks register; and
  # the private routes, because a task with no path to NAT cannot pull its
  # image and fails in a way that looks like a broken deployment.
  depends_on = [aws_lb_listener.https, aws_route_table_association.private]
}

# --- worker ----------------------------------------------------------------

resource "aws_ecs_task_definition" "worker" {
  family                   = "${local.name}-worker"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.worker_cpu
  memory                   = var.worker_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.worker_task.arn

  runtime_platform {
    operating_system_family = local.runtime.operating_system_family
    cpu_architecture        = local.runtime.cpu_architecture
  }

  container_definitions = jsonencode([{
    name      = "worker"
    image     = local.image
    essential = true

    # Prefork is fine here; --pool=solo in the repo is a macOS workaround for
    # Metal failing after fork. Concurrency stays at 1 because each child holds
    # its own copy of the embedding model.
    command = [
      "celery", "-A", "api.ingestion.celery_app", "worker",
      "--loglevel=info", "--concurrency=1",
    ]

    environment = concat(local.common_env, [
      { name = "NEO4J_URI", value = "bolt://${aws_instance.neo4j.private_ip}:7687" },
    ])

    secrets = [
      local.db_secret,
      { name = "NEO4J_AUTH", valueFrom = aws_secretsmanager_secret.neo4j_auth.arn },
      # No JWT_SECRET. The worker parses untrusted PubTator documents and has
      # no business being able to mint admin tokens; api/auth/config.py is a
      # separate settings class precisely so its absence cannot stop the worker
      # booting.
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.worker.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "worker"
      }
    }
  }])
}

resource "aws_ecs_service" "worker" {
  name            = "${local.name}-worker"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.worker.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.worker.id]
    assign_public_ip = false
  }

  # A task is acknowledged only when it finishes, so stopping the old worker
  # before starting the new one returns in-flight work to the queue rather than
  # having two workers write the same rows concurrently.
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  depends_on = [aws_route_table_association.private]
}

# --- migrations ------------------------------------------------------------

# A task definition with no service. Run it with `aws ecs run-task` before
# updating the services -- the image now carries alembic.ini, which is what
# makes this possible at all.
resource "aws_ecs_task_definition" "migrate" {
  family                   = "${local.name}-migrate"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 512
  memory                   = 1024
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.worker_task.arn

  runtime_platform {
    operating_system_family = local.runtime.operating_system_family
    cpu_architecture        = local.runtime.cpu_architecture
  }

  container_definitions = jsonencode([{
    name      = "migrate"
    image     = local.image
    essential = true
    command   = ["alembic", "upgrade", "head"]

    environment = local.common_env
    secrets     = [local.db_secret]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.migrate.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "migrate"
      }
    }
  }])
}

# --- ECS task execution: what the agent needs to *start* a task ------------

resource "aws_iam_role" "task_execution" {
  name = "${local.name}-task-execution"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Secret injection happens in the agent, before the container starts, so this
# permission belongs to the execution role rather than the task role. Scoped to
# these three ARNs: a wildcard here would let any task in the account's
# execution path read every secret.
resource "aws_iam_role_policy" "task_execution_secrets" {
  name = "${local.name}-read-secrets"
  role = aws_iam_role.task_execution.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = ["secretsmanager:GetSecretValue"]
      Resource = [
        aws_secretsmanager_secret.jwt.arn,
        aws_secretsmanager_secret.database_url.arn,
        aws_secretsmanager_secret.neo4j_auth.arn,
      ]
    }]
  })
}

# --- task roles: what the running application may do -----------------------
#
# Both are empty of AWS permissions. The app talks to Postgres, Redis, Neo4j
# and NCBI, and to no AWS API -- so an empty role is the accurate one, and a
# separate role per task means a future permission cannot be granted to both by
# accident.

resource "aws_iam_role" "api_task" {
  name = "${local.name}-api-task"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role" "worker_task" {
  name = "${local.name}-worker-task"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

# --- Neo4j instance --------------------------------------------------------

resource "aws_iam_role" "neo4j" {
  name = "${local.name}-neo4j"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "neo4j_secret" {
  name = "${local.name}-neo4j-secret"
  role = aws_iam_role.neo4j.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      Resource = [aws_secretsmanager_secret.neo4j_auth.arn]
    }]
  })
}

# Session Manager, so the box can be reached without a key pair, a bastion, or
# port 22 open anywhere.
resource "aws_iam_role_policy_attachment" "neo4j_ssm" {
  role       = aws_iam_role.neo4j.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "neo4j" {
  name = "${local.name}-neo4j"
  role = aws_iam_role.neo4j.name
}

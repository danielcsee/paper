output "app_url" {
  value = "https://${var.domain_name}"
}

output "alb_dns_name" {
  description = "The load balancer's own name, for reaching the app before DNS has propagated."
  value       = aws_lb.main.dns_name
}

output "ecr_repository_url" {
  value = aws_ecr_repository.app.repository_url
}

output "jwt_secret_arn" {
  description = "JWT signing secret generated and populated securely during terraform apply."
  value       = aws_secretsmanager_secret.jwt.arn
}

output "migrate_command" {
  description = "Run before every deploy that adds a migration."
  value       = <<-EOT
    aws ecs run-task \
      --cluster ${aws_ecs_cluster.main.name} \
      --task-definition ${aws_ecs_task_definition.migrate.arn} \
      --launch-type FARGATE \
      --network-configuration 'awsvpcConfiguration={subnets=[${join(",", aws_subnet.private[*].id)}],securityGroups=[${aws_security_group.worker.id}],assignPublicIp=DISABLED}'
  EOT
}

# Release coordinates are outputs rather than GitHub environment values: each
# apply can register a new immutable task-definition revision.
output "ecs_cluster_name" {
  value = aws_ecs_cluster.main.name
}

output "api_service_name" {
  value = aws_ecs_service.api.name
}

output "worker_service_name" {
  value = aws_ecs_service.worker.name
}

output "api_task_definition_arn" {
  value = aws_ecs_task_definition.api.arn
}

output "worker_task_definition_arn" {
  value = aws_ecs_task_definition.worker.arn
}

output "migrate_task_definition_arn" {
  value = aws_ecs_task_definition.migrate.arn
}

output "migration_network_configuration" {
  value = jsonencode({
    awsvpcConfiguration = {
      subnets        = aws_subnet.private[*].id
      securityGroups = [aws_security_group.worker.id]
      assignPublicIp = "DISABLED"
    }
  })
}

output "api_release_desired_count" {
  value = var.api_desired_count
}

output "worker_release_desired_count" {
  value = var.worker_desired_count
}

output "neo4j_private_ip" {
  description = "Reachable from the worker only. Use Session Manager to get a shell on the host."
  value       = aws_instance.neo4j.private_ip
}

output "budget_shutdown_latch" {
  description = "Set this SSM parameter to false before deliberately restarting an app stopped by the budget guardrail."
  value       = aws_ssm_parameter.budget_shutdown_latch.name
}

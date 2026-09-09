output "name_servers" {
  description = "Copy these to your registrar to delegate the zone. The certificate cannot validate until you do."
  value       = aws_route53_zone.main.name_servers
}

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
  description = "Empty on creation. Put a value in it before the API will start."
  value       = aws_secretsmanager_secret.jwt.arn
}

output "migrate_command" {
  description = "Run before every deploy that adds a migration."
  value       = <<-EOT
    aws ecs run-task \
      --cluster ${aws_ecs_cluster.main.name} \
      --task-definition ${aws_ecs_task_definition.migrate.family} \
      --launch-type FARGATE \
      --network-configuration 'awsvpcConfiguration={subnets=[${join(",", aws_subnet.private[*].id)}],securityGroups=[${aws_security_group.worker.id}],assignPublicIp=DISABLED}'
  EOT
}

output "neo4j_private_ip" {
  description = "Reachable from the worker only. Use Session Manager to get a shell on the host."
  value       = aws_instance.neo4j.private_ip
}

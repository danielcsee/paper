# Neo4j on a single EC2 instance.
#
# There is no managed Neo4j on AWS, and Neo4j's own AuraDB Free tier does not
# include the Graph Data Science plugin this project installs for PageRank, so
# self-hosting is the only option that keeps the graph features working.
#
# The worker is the only client: no serving route reads the graph. But it is
# not optional either -- the graph stage is what makes an import count as
# "imported" -- so this has to be up before the pipeline will complete.

data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

resource "aws_instance" "neo4j" {
  ami                    = data.aws_ssm_parameter.al2023.value
  instance_type          = var.neo4j_instance_type
  subnet_id              = aws_subnet.private[0].id
  vpc_security_group_ids = [aws_security_group.neo4j.id]
  iam_instance_profile   = aws_iam_instance_profile.neo4j.name

  root_block_device {
    volume_size = var.neo4j_root_volume_gb
    volume_type = "gp3"
    encrypted   = true
  }

  # The password is fetched from Secrets Manager at boot rather than passed in
  # here: user_data is readable by anything that can reach the instance
  # metadata service, so a secret placed in it is not a secret.
  user_data = <<-BASH
    #!/bin/bash
    set -euo pipefail

    dnf install -y docker
    systemctl enable --now docker

    NEO4J_AUTH="$(aws secretsmanager get-secret-value \
      --secret-id ${aws_secretsmanager_secret.neo4j_auth.id} \
      --region ${var.region} --query SecretString --output text)"

    mkdir -p /var/lib/neo4j/data /var/lib/neo4j/logs

    docker run -d --name neo4j --restart unless-stopped \
      -p 7687:7687 -p 7474:7474 \
      -v /var/lib/neo4j/data:/data \
      -v /var/lib/neo4j/logs:/logs \
      -e NEO4J_AUTH="$NEO4J_AUTH" \
      -e NEO4J_PLUGINS='["graph-data-science"]' \
      -e NEO4J_dbms_security_procedures_unrestricted='gds.*' \
      -e NEO4J_dbms_security_procedures_allowlist='gds.*' \
      -e NEO4J_server_default__listen__address=0.0.0.0 \
      neo4j:5.26-community
  BASH

  # Changing user_data must rebuild the instance, or the change is written and
  # never executed -- which reads as "the deploy did nothing".
  user_data_replace_on_change = true

  # IMDSv2 only.
  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  tags = { Name = "${local.name}-neo4j" }
}

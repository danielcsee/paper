# terraform

The AWS deployment: VPC, RDS, ElastiCache, a Neo4j host, two Fargate services,
an ALB with TLS, and a migration task. Roughly $135/month idle — see the
deployment plan for where that goes and how little it moves with usage.

State lives in S3 with **native S3 locking** (`use_lockfile`), not a DynamoDB
table. Terraform 1.10 added locking through S3 conditional writes and 1.11
deprecated the DynamoDB argument, so `required_version` is `>= 1.10`.

## Layout

| File | Holds |
| --- | --- |
| `bootstrap/` | The state bucket. Applied once, with local state. |
| `versions.tf` | Provider and Terraform versions, and the S3 backend. |
| `main.tf` | Provider, shared locals, the environment both tasks get. |
| `network.tf` | VPC, subnets, NAT, and every security group. |
| `rds.tf` `elasticache.tf` `neo4j.tf` | The three datastores. |
| `ecs.tf` | Cluster, API and worker services, and the migration task. |
| `alb.tf` | Route 53 zone, ACM certificate, load balancer, listeners. |
| `iam.tf` `secrets.tf` `waf.tf` `ecr.tf` | Supporting resources. |

## First apply

Your domain is registered elsewhere, so this is a two-stage apply: the
certificate cannot validate until the registrar delegates to the zone
Terraform creates, and `aws_acm_certificate_validation` waits until it does.

```bash
cd terraform/bootstrap && terraform init && terraform apply   # state bucket
# paste the backend_config output into ../versions.tf, uncomment, then:
cd .. && terraform init

cp terraform.tfvars.example terraform.tfvars   # fill in domain and image tag

# 1. the zone, so you have name servers to delegate
terraform apply -target=aws_route53_zone.main
terraform output name_servers                  # paste these at your registrar

# 2. wait for delegation to propagate, then everything else
terraform apply
```

Then the parts Terraform deliberately does not do:

```bash
# The signing key. Terraform creates an empty secret; the value never passes
# through it, so it never lands in state.
aws secretsmanager put-secret-value \
  --secret-id "$(terraform output -raw jwt_secret_arn)" \
  --secret-string "$(python3 -c 'import secrets; print(secrets.token_urlsafe(48))')"

# Build, push, migrate. The image now carries alembic.ini, which is what makes
# the migration task possible.
docker build --platform linux/arm64 \
  --build-arg SCITERM_VERSION=$(git rev-parse --short HEAD) \
  -t "$(terraform output -raw ecr_repository_url):$(git rev-parse --short HEAD)" .
# ...docker push, then:
terraform output -raw migrate_command | bash

# Force the services onto the new image
aws ecs update-service --cluster sciterm --service sciterm-api --force-new-deployment
```

Finally, bootstrap the admin account and issue codes. No secret is provisioned
for this: the server already trusts the public key in `api/authorized_keys/`.

```bash
./scripts/admin.sh rotate-admin https://your.domain
./scripts/admin.sh codes 10 https://your.domain
```

## Decisions worth knowing

**One NAT gateway, not one per AZ.** It is the largest single line on the bill.
Both private subnets route through the one in AZ-a, so an outage there takes
egress down for everything. A one-line change if that ever matters.

**No VPC endpoints.** They would cut NAT data charges on ECR pulls and log
writes, but four interface endpoints cost more per month than the traffic they
would save at this scale. Revisit if task restarts become frequent.

**The API is not given a route to Neo4j.** Only the worker's security group can
reach it, because only `api/ingestion/tasks.py` and `backfill_graph.py` import
`api.graph`. Neo4j is still required — the graph stage is what makes an import
count as imported — but nothing serving a request touches it.

**The worker is not given `JWT_SECRET`.** It parses untrusted PubTator
documents and has no business minting admin tokens. `api/auth/config.py` is a
separate settings class so that its absence cannot stop the worker booting.

**The load balancer health check is `/health`, not `/health/ready`.** Readiness
checks Postgres; if the database went down, every task would fail at once, the
whole target group would drain, and visitors would get the balancer's 503
instead of the app's error.

**The document cache is off.** Its eviction policy cannot share a node with the
broker without letting cached documents evict queued tasks, and a second node
is real money for an optimisation whose value scales with import volume —
which access codes cap. Add `REDIS_CACHE_URL` and a second cluster if NCBI's
rate limit starts to bite.

**Datastore passwords are in state; the signing key is not.** Terraform has to
know the database password to create the database, so it lands in state — which
is why the bucket is encrypted, versioned and blocked from public access. The
signing key has no such excuse, so Terraform only creates the empty container.

## Tearing down

`destroy_friendly = true` (the default) skips the RDS final snapshot and leaves
deletion protection off, so `terraform destroy` completes unattended. That
suits applying before interviews and destroying after. Set it to `false` the
moment the database holds anything you would miss. The state bucket in
`bootstrap/` has `prevent_destroy` and survives either way.

## Not yet here

CI/CD. The pipeline is build → push → migrate → update services, and the
migrate step is the one that must not be skipped. There is also no automated
test suite in this repository for CI to run.

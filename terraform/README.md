# terraform

The AWS deployment: VPC, RDS, ElastiCache, a Neo4j host, two Fargate services,
an ALB with TLS, and a migration task. Roughly $135/month idle — see the
deployment plan for where that goes and how little it moves with usage.

State lives in S3 with **native S3 locking** (`use_lockfile`), not a DynamoDB
table. The application stack requires Terraform `>= 1.11` so it can generate
the JWT signing key ephemerally and write it to Secrets Manager without putting
the value in the plan or state. The bootstrap stack still supports 1.10+.

## Layout

| File | Holds |
| --- | --- |
| `bootstrap/` | The state bucket. Applied once, with local state. |
| `versions.tf` | Provider and Terraform versions, and the S3 backend. |
| `main.tf` | Provider, shared locals, the environment both tasks get. |
| `network.tf` | VPC, subnets, NAT, and every security group. |
| `rds.tf` `elasticache.tf` `neo4j.tf` | The three datastores. |
| `ecs.tf` | Cluster, API and worker services, and the migration task. |
| `alb.tf` | Cloudflare DNS, ACM certificate, load balancer, listeners. |
| `iam.tf` `secrets.tf` `waf.tf` `ecr.tf` | Supporting resources. |

## Bootstrap and deployment

Cloudflare remains the authoritative DNS provider. Terraform uses a narrowly
scoped API token to create the app CNAME and AWS ACM validation CNAME, so DNS
and certificate validation complete during the normal apply.

```bash
cd terraform/bootstrap && terraform init && terraform apply   # state bucket
# paste the backend_config output into ../versions.tf, uncomment, then:
cd .. && terraform init

# The image destination must exist before the first GitHub build can push.
terraform apply -target=aws_ecr_repository.app
```

Set `domain_name` to the full hostname (for example, `app.example.com`) and
copy the Zone ID from the domain's Cloudflare Overview page into
`cloudflare_zone_id`. The generated records are DNS-only: TLS and WAF remain
on AWS, and the existing WAF source-IP rate limit continues to see visitors'
real IP addresses. If a DNS record with the same app hostname already exists,
delete it or import it before applying so Terraform does not collide with it.

The `deploy` GitHub environment supplies the AWS and Terraform variables and
the Cloudflare token documented in `.github/workflows/deploy.yml`. A manual
dispatch or a push to `stage` builds the ARM64 image, tags it with the commit SHA,
pushes it to ECR, applies Terraform, runs migrations, and only then updates the
services. Terraform creates a service at zero tasks on its first apply, so the
first application process cannot start against an unmigrated database.

Terraform also creates the JWT signing key before registering the API task
definition. Its value is never printed or stored in Terraform state.

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
signing key is generated ephemerally and passed to Secrets Manager through a
write-only argument. Increment `jwt_secret_version` to rotate it and register a
new API task definition revision. Rotation invalidates all existing JWTs.

**Terraform defines services; GitHub releases them.** Terraform owns each ECS
service's infrastructure but ignores its live task-definition revision and
desired count. GitHub captures the current release, applies infrastructure,
runs the exact migration task revision, and checks its exit code before moving
the services to the exact API and worker revisions. Failed cutovers roll back
to the captured revisions and counts; ECS deployment circuit breakers provide
a second rollback layer. Previous task definitions remain active so that exact
rollback target can still launch replacement tasks. Schema migrations must
remain compatible with the old application while that old revision stays live.

## Tearing down

`destroy_friendly = true` (the default) skips the RDS final snapshot and leaves
deletion protection off, so `terraform destroy` completes unattended. That
suits applying before interviews and destroying after. Set it to `false` the
moment the database holds anything you would miss. The state bucket in
`bootstrap/` has `prevent_destroy` and survives either way.
API and worker task-definition revisions also remain registered as zero-cost
rollback metadata and can be deregistered manually after the application is
destroyed.

## CI/CD

`.github/workflows/deploy.yml` serializes deployments and runs build → push →
Terraform apply → migrate → service cutover → health verification. It refuses
to run while the budget shutdown latch is set. There is not yet an automated
application test suite for CI to run before building.

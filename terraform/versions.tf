terraform {
  # 1.10 introduced native S3 state locking. The application stack requires
  # 1.11+ for ephemeral values and write-only resource arguments.
  required_version = ">= 1.11"

  required_providers {
    aws        = { source = "hashicorp/aws", version = ">= 5.89, < 6.0" }
    archive    = { source = "hashicorp/archive", version = "~> 2.7" }
    cloudflare = { source = "cloudflare/cloudflare", version = "~> 5.24" }
    random     = { source = "hashicorp/random", version = ">= 3.7.1, < 4.0" }
  }

  # Created by ./bootstrap, which runs once with local state. Fill in the
  # bucket name and uncomment, then `terraform init -migrate-state`.
  #
  backend "s3" {
    bucket       = "sciterm-tfstate-052937361440"
    key          = "sciterm/terraform.tfstate"
    region       = "us-east-2"
    encrypt      = true
    use_lockfile = true
  }
}

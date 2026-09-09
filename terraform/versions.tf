terraform {
  # 1.10 introduced native S3 state locking via conditional writes, and 1.11
  # deprecated the DynamoDB lock table. `use_lockfile` below needs 1.10+.
  required_version = ">= 1.10"

  required_providers {
    aws    = { source = "hashicorp/aws", version = "~> 5.70" }
    archive = { source = "hashicorp/archive", version = "~> 2.7" }
    random = { source = "hashicorp/random", version = "~> 3.6" }
  }

  # Created by ./bootstrap, which runs once with local state. Fill in the
  # bucket name and uncomment, then `terraform init -migrate-state`.
  #
  # backend "s3" {
  #   bucket       = "sciterm-tfstate-<account-id>"
  #   key          = "sciterm/terraform.tfstate"
  #   region       = "us-east-1"
  #   encrypt      = true
  #   use_lockfile = true # S3 conditional writes; no DynamoDB table needed
  # }
}

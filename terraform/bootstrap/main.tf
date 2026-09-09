# State bucket, applied once with local state before the main stack.
#
# Deliberately separate: a bucket that holds the state describing itself is a
# chicken-and-egg on the first apply and an unrecoverable tangle on destroy.
# This directory is tiny and effectively permanent; the state file it produces
# can be committed or thrown away, since re-applying is idempotent.
#
# No DynamoDB lock table. Terraform 1.10 locks through S3 conditional writes
# (`use_lockfile`), and 1.11 deprecated the DynamoDB argument.

terraform {
  required_version = ">= 1.10"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.70" }
  }
}

provider "aws" {
  region = var.region
}

variable "region" {
  type        = string
  default     = "us-east-1"
  description = "Region for the state bucket. Must match the backend block."
}

variable "project" {
  type    = string
  default = "sciterm"
}

data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "state" {
  bucket = "${var.project}-tfstate-${data.aws_caller_identity.current.account_id}"

  # The bucket outlives every `terraform destroy` of the main stack, so losing
  # it to a stray destroy here would strand the real infrastructure.
  lifecycle {
    prevent_destroy = true
  }
}

# Versioning is not optional with S3 locking: it is how a corrupted or
# half-written state file can be rolled back to the previous good version.
resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "state" {
  bucket                  = aws_s3_bucket.state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

output "backend_config" {
  description = "Paste into the backend \"s3\" block in ../versions.tf."
  value       = <<-EOT
    bucket       = "${aws_s3_bucket.state.id}"
    key          = "${var.project}/terraform.tfstate"
    region       = "${var.region}"
    encrypt      = true
    use_lockfile = true
  EOT
}

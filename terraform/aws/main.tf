# terraform/aws/main.tf
#
# Provisions AWS resources used by caboose-mcp:
#   - IAM user + policy for Bedrock access (Claude models)
#   - S3 bucket for cloudsync (config backups)
#   - ECR repository for the Docker image
#
# Prerequisites:
#   aws configure  (or set AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)
#   terraform init
#   terraform apply

terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Uncomment to store state in S3 (recommended for shared/production use):
  # backend "s3" {
  #   bucket = "your-terraform-state-bucket"
  #   key    = "caboose-mcp/terraform.tfstate"
  #   region = "us-east-1"
  # }
}

provider "aws" {
  region = var.aws_region
}

# ─── IAM: Bedrock access ──────────────────────────────────────────────────────

resource "aws_iam_user" "caboose_cli" {
  name = var.iam_user_name
  path = "/"

  tags = local.common_tags
}

resource "aws_iam_access_key" "caboose_cli" {
  user = aws_iam_user.caboose_cli.name
}

resource "aws_iam_user_policy" "bedrock" {
  name = "caboose-mcp-bedrock"
  user = aws_iam_user.caboose_cli.name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "BedrockInference"
        Effect = "Allow"
        Action = [
          "bedrock:InvokeModel",
          "bedrock:InvokeModelWithResponseStream",
          "bedrock:ListFoundationModels",
          "bedrock:ListInferenceProfiles",
          "bedrock:GetFoundationModel",
        ]
        Resource = "*"
      }
    ]
  })
}

# ─── S3: cloudsync backup bucket ──────────────────────────────────────────────

resource "aws_s3_bucket" "cloudsync" {
  bucket = var.cloudsync_bucket_name

  tags = local.common_tags
}

resource "aws_s3_bucket_versioning" "cloudsync" {
  bucket = aws_s3_bucket.cloudsync.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "cloudsync" {
  bucket = aws_s3_bucket.cloudsync.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "cloudsync" {
  bucket = aws_s3_bucket.cloudsync.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_iam_user_policy" "s3_cloudsync" {
  name = "caboose-mcp-cloudsync-s3"
  user = aws_iam_user.caboose_cli.name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "CloudsyncReadWrite"
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket",
        ]
        Resource = [
          aws_s3_bucket.cloudsync.arn,
          "${aws_s3_bucket.cloudsync.arn}/*",
        ]
      }
    ]
  })
}

# ─── ECR: Docker image registry ───────────────────────────────────────────────

resource "aws_ecr_repository" "caboose_mcp" {
  name                 = "caboose-mcp"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = local.common_tags
}

resource "aws_ecr_lifecycle_policy" "caboose_mcp" {
  repository = aws_ecr_repository.caboose_mcp.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Keep last 10 images"
        selection = {
          tagStatus   = "any"
          countType   = "imageCountMoreThan"
          countNumber = 10
        }
        action = { type = "expire" }
      }
    ]
  })
}

locals {
  common_tags = {
    Project     = "caboose-mcp"
    ManagedBy   = "terraform"
    Environment = var.environment
  }
}

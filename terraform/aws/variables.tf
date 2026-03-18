variable "aws_region" {
  description = "AWS region for all resources."
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Deployment environment tag (e.g. personal, prod)."
  type        = string
  default     = "personal"
}

variable "iam_user_name" {
  description = "Name of the IAM user that gets Bedrock + S3 access."
  type        = string
  default     = "caboose_cli"
}

variable "cloudsync_bucket_name" {
  description = "S3 bucket name for caboose-mcp cloudsync backups. Must be globally unique."
  type        = string
  # Override in terraform.tfvars:  cloudsync_bucket_name = "my-caboose-cloudsync"
}

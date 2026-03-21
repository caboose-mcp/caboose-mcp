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
  description = "S3 bucket name for fafb cloudsync backups. Must be globally unique."
  type        = string
  # Override in terraform.tfvars:  cloudsync_bucket_name = "my-caboose-cloudsync"
}

variable "aws_profile" {
  description = "Local AWS CLI profile to use. Leave empty to use the default credential chain."
  type        = string
  default     = ""
}

variable "slack_bot_channels" {
  description = "Comma-separated Slack channel IDs the bot responds in (DMs always work)."
  type        = string
  default     = ""
}

variable "discord_bot_channels" {
  description = "Comma-separated Discord channel IDs the bot responds in."
  type        = string
  default     = ""
}

variable "domain_name" {
  description = "Custom domain for the MCP HTTP server (e.g. mcp.chrismarasco.io)."
  type        = string
  default     = "mcp.chrismarasco.io"
}

variable "route53_zone_id" {
  description = "Route53 hosted zone ID for domain_name. Leave empty if DNS is managed elsewhere — you'll get validation CNAMEs to add manually."
  type        = string
  default     = ""
}

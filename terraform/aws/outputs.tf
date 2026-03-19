output "iam_user_name" {
  description = "IAM user name."
  value       = aws_iam_user.caboose_cli.name
}


output "cloudsync_bucket_name" {
  description = "S3 bucket name — add to .env as CLOUDSYNC_S3_BUCKET."
  value       = aws_s3_bucket.cloudsync.bucket
}

output "ecr_repository_url" {
  description = "ECR repository URL — tag and push your Docker image here."
  value       = aws_ecr_repository.caboose_mcp.repository_url
}

output "mcp_server_url" {
  description = "MCP HTTPS endpoint."
  value       = "https://${var.domain_name}/mcp"
}

output "alb_dns_name" {
  description = "Raw ALB DNS name — add a CNAME here if not using Route53."
  value       = aws_lb.serve.dns_name
}

output "acm_certificate_arn" {
  description = "ACM certificate ARN."
  value       = aws_acm_certificate.mcp.arn
}

output "route53_zone_id" {
  description = "Route53 hosted zone ID — consumed by caboose-mcp-ui terraform via remote_state."
  value       = var.route53_zone_id
}

output "acm_validation_records" {
  description = "DNS validation CNAMEs to add if route53_zone_id is empty."
  value = {
    for dvo in aws_acm_certificate.mcp.domain_validation_options :
    dvo.domain_name => {
      name  = dvo.resource_record_name
      type  = dvo.resource_record_type
      value = dvo.resource_record_value
    }
  }
}

output "secrets_manager_arn" {
  description = "Secrets Manager ARN — populate with your env vars before deploying."
  value       = aws_secretsmanager_secret.env.arn
}

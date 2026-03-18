output "iam_user_name" {
  description = "IAM user name."
  value       = aws_iam_user.caboose_cli.name
}

output "aws_access_key_id" {
  description = "AWS access key ID — add to .env as AWS_ACCESS_KEY_ID."
  value       = aws_iam_access_key.caboose_cli.id
  sensitive   = true
}

output "aws_secret_access_key" {
  description = "AWS secret access key — add to .env as AWS_SECRET_ACCESS_KEY."
  value       = aws_iam_access_key.caboose_cli.secret
  sensitive   = true
}

output "cloudsync_bucket_name" {
  description = "S3 bucket name — add to .env as CLOUDSYNC_S3_BUCKET."
  value       = aws_s3_bucket.cloudsync.bucket
}

output "ecr_repository_url" {
  description = "ECR repository URL for docker push."
  value       = aws_ecr_repository.caboose_mcp.repository_url
}

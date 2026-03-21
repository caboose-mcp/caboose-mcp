# terraform/aws

Provisions AWS resources used by fafb.

## Resources

| Resource | Purpose |
|----------|---------|
| `aws_iam_user` | IAM user (`caboose_cli`) with Bedrock + S3 access |
| `aws_iam_access_key` | Access key for the IAM user |
| `aws_iam_user_policy` (bedrock) | Allows `bedrock:InvokeModel*`, `ListFoundationModels`, `ListInferenceProfiles` |
| `aws_s3_bucket` | Encrypted, versioned bucket for `cloudsync_push/pull` |
| `aws_iam_user_policy` (s3) | Scoped S3 read/write on the cloudsync bucket only |
| `aws_ecr_repository` | Docker image registry; lifecycle policy keeps last 10 images |

## Prerequisites

- Terraform >= 1.6
- AWS credentials with IAM + S3 + ECR permissions
- `aws configure` or `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` in env

## Usage

```bash
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars — set cloudsync_bucket_name to something globally unique

terraform init
terraform plan    # review before applying
terraform apply
```

After apply, retrieve the generated credentials:

```bash
terraform output -raw aws_access_key_id
terraform output -raw aws_secret_access_key
```

Add these to your `.env`:
```
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
CLOUDSYNC_S3_BUCKET=...   # from terraform output cloudsync_bucket_name
```

## State

State is stored locally by default (`terraform.tfstate` — gitignored).
To use remote state, uncomment the `backend "s3"` block in `main.tf` and
create a separate state bucket manually first.

## Destroying

```bash
terraform destroy
```

This deletes the S3 bucket (only if empty), ECR repo, and IAM user.

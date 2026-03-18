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
  region  = var.aws_region
  profile = var.aws_profile != "" ? var.aws_profile : null
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

# ─── Data sources ─────────────────────────────────────────────────────────────

data "aws_vpc" "default" { default = true }

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# ─── Secrets Manager ──────────────────────────────────────────────────────────
# Create the secret, then populate it:
#   aws secretsmanager put-secret-value \
#     --secret-id caboose-mcp/env \
#     --secret-string '{"ANTHROPIC_API_KEY":"...","SLACK_TOKEN":"...","SLACK_APP_TOKEN":"...","DISCORD_TOKEN":"...","MCP_AUTH_TOKEN":"..."}'

resource "aws_secretsmanager_secret" "env" {
  name        = "caboose-mcp/env"
  description = "caboose-mcp runtime secrets"
  tags        = local.common_tags
}

# ─── CloudWatch log groups ────────────────────────────────────────────────────

resource "aws_cloudwatch_log_group" "bots" {
  name              = "/ecs/caboose-mcp/bots"
  retention_in_days = 30
  tags              = local.common_tags
}

resource "aws_cloudwatch_log_group" "serve" {
  name              = "/ecs/caboose-mcp/serve"
  retention_in_days = 30
  tags              = local.common_tags
}

# ─── ECS cluster ──────────────────────────────────────────────────────────────

resource "aws_ecs_cluster" "main" {
  name = "caboose-mcp"
  tags = local.common_tags
}

# ─── IAM: ECS execution role (agent: pull image, push logs, read secrets) ─────

resource "aws_iam_role" "ecs_exec" {
  name = "caboose-mcp-ecs-exec"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy_attachment" "ecs_exec_managed" {
  role       = aws_iam_role.ecs_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "ecs_exec_secrets" {
  name = "read-caboose-mcp-secrets"
  role = aws_iam_role.ecs_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      Resource = aws_secretsmanager_secret.env.arn
    }]
  })
}

# ─── IAM: ECS task role (app permissions: S3 cloudsync) ──────────────────────

resource "aws_iam_role" "ecs_task" {
  name = "caboose-mcp-ecs-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy" "ecs_task_s3" {
  name = "cloudsync-s3"
  role = aws_iam_role.ecs_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"]
      Resource = [
        aws_s3_bucket.cloudsync.arn,
        "${aws_s3_bucket.cloudsync.arn}/*",
      ]
    }]
  })
}

# ─── Security groups ──────────────────────────────────────────────────────────

resource "aws_security_group" "alb" {
  name        = "caboose-mcp-alb"
  description = "ALB: allow inbound HTTP and HTTPS"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = local.common_tags
}

resource "aws_security_group" "ecs_serve" {
  name        = "caboose-mcp-serve"
  description = "ECS serve task: allow traffic from ALB on 8080"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = local.common_tags
}

resource "aws_security_group" "ecs_bots" {
  name        = "caboose-mcp-bots"
  description = "ECS bots task: outbound only"
  vpc_id      = data.aws_vpc.default.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = local.common_tags
}

# ─── ALB ──────────────────────────────────────────────────────────────────────

resource "aws_lb" "serve" {
  name               = "caboose-mcp-serve"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = data.aws_subnets.default.ids
  tags               = local.common_tags
}

resource "aws_lb_target_group" "serve" {
  name        = "caboose-mcp-serve"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = data.aws_vpc.default.id
  target_type = "ip"

  health_check {
    path    = "/mcp"
    matcher = "200,405"
  }

  tags = local.common_tags
}

# ─── ACM certificate ──────────────────────────────────────────────────────────

resource "aws_acm_certificate" "mcp" {
  domain_name       = var.domain_name
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }

  tags = local.common_tags
}

# Route53 DNS validation records (only if route53_zone_id is set)
data "aws_route53_zone" "main" {
  count   = var.route53_zone_id != "" ? 1 : 0
  zone_id = var.route53_zone_id
}

resource "aws_route53_record" "cert_validation" {
  for_each = var.route53_zone_id != "" ? {
    for dvo in aws_acm_certificate.mcp.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      type   = dvo.resource_record_type
      record = dvo.resource_record_value
    }
  } : {}

  zone_id = var.route53_zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = 60
  records = [each.value.record]
}

resource "aws_acm_certificate_validation" "mcp" {
  count                   = var.route53_zone_id != "" ? 1 : 0
  certificate_arn         = aws_acm_certificate.mcp.arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

# Route53 A record pointing the domain at the ALB
resource "aws_route53_record" "mcp" {
  count   = var.route53_zone_id != "" ? 1 : 0
  zone_id = var.route53_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_lb.serve.dns_name
    zone_id                = aws_lb.serve.zone_id
    evaluate_target_health = true
  }
}

# ─── ALB listeners ────────────────────────────────────────────────────────────

# HTTP → HTTPS redirect
resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.serve.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.serve.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate.mcp.arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.serve.arn
  }

  depends_on = [aws_acm_certificate_validation.mcp]
}

# ─── ECS task definitions ─────────────────────────────────────────────────────

resource "aws_ecs_task_definition" "bots" {
  family                   = "caboose-mcp-bots"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ecs_exec.arn
  task_role_arn            = aws_iam_role.ecs_task.arn

  container_definitions = jsonencode([{
    name      = "bots"
    image     = "${aws_ecr_repository.caboose_mcp.repository_url}:latest"
    command   = ["--bots"]
    essential = true

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.bots.name
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "ecs"
      }
    }

    secrets = [
      { name = "ANTHROPIC_API_KEY", valueFrom = "${aws_secretsmanager_secret.env.arn}:ANTHROPIC_API_KEY::" },
      { name = "SLACK_TOKEN",       valueFrom = "${aws_secretsmanager_secret.env.arn}:SLACK_TOKEN::" },
      { name = "SLACK_APP_TOKEN",   valueFrom = "${aws_secretsmanager_secret.env.arn}:SLACK_APP_TOKEN::" },
      { name = "DISCORD_TOKEN",     valueFrom = "${aws_secretsmanager_secret.env.arn}:DISCORD_TOKEN::" },
    ]

    environment = [
      { name = "SLACK_BOT_CHANNELS",   value = var.slack_bot_channels },
      { name = "DISCORD_BOT_CHANNELS", value = var.discord_bot_channels },
    ]
  }])

  tags = local.common_tags
}

resource "aws_ecs_task_definition" "serve" {
  family                   = "caboose-mcp-serve"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ecs_exec.arn
  task_role_arn            = aws_iam_role.ecs_task.arn

  container_definitions = jsonencode([{
    name      = "serve"
    image     = "${aws_ecr_repository.caboose_mcp.repository_url}:latest"
    command   = ["--serve", ":8080"]
    essential = true

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.serve.name
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "ecs"
      }
    }

    secrets = [
      { name = "MCP_AUTH_TOKEN",   valueFrom = "${aws_secretsmanager_secret.env.arn}:MCP_AUTH_TOKEN::" },
      { name = "ANTHROPIC_API_KEY", valueFrom = "${aws_secretsmanager_secret.env.arn}:ANTHROPIC_API_KEY::" },
    ]
  }])

  tags = local.common_tags
}

# ─── ECS services ─────────────────────────────────────────────────────────────

resource "aws_ecs_service" "bots" {
  name            = "caboose-mcp-bots"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.bots.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = data.aws_subnets.default.ids
    security_groups  = [aws_security_group.ecs_bots.id]
    assign_public_ip = true
  }

  tags = local.common_tags
}

resource "aws_ecs_service" "serve" {
  name            = "caboose-mcp-serve"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.serve.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = data.aws_subnets.default.ids
    security_groups  = [aws_security_group.ecs_serve.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.serve.arn
    container_name   = "serve"
    container_port   = 8080
  }

  depends_on = [aws_lb_listener.http]

  tags = local.common_tags
}

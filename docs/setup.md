# Setup Guide

Two deployment paths: **local** (Pi or any Linux machine, all 108 tools) and **hosted** (AWS ECS, 88 cloud-safe tools).

---

## Local Setup

Runs directly on your machine. All tools available including Docker, shell, Bambu printer, Blender, and Chezmoi.

### 1. Install prerequisites

```bash
# Go 1.24+ (https://go.dev/dl/)
go version   # verify

# Optional local tools
docker --version
chezmoi --version
blender --version
gpg --version
```

### 2. Clone and build

```bash
git clone https://github.com/fafb/fafb.git
cd fafb/packages/server
go build -o fafb .
```

### 3. Configure with the setup wizard

```bash
./fafb --setup
```

This interactive wizard walks through every config option and writes a `.env` file. Press Enter to keep any default, type `-` to clear a value.

**Sections covered:**
- Core (`CLAUDE_DIR`, `GPG_KEY_ID`)
- Messaging (`SLACK_TOKEN`, `DISCORD_TOKEN`)
- n8n Integration (`N8N_WEBHOOK_URL`, `N8N_API_KEY`)
- GitHub (`GITHUB_TOKEN`)
- Databases (`POSTGRES_URL`, `MONGO_URL`)
- Bambu 3D Printer (`BAMBU_IP`, `BAMBU_ACCESS_CODE`, `BAMBU_SERIAL`)
- Greptile (`GREPTILE_API_KEY`)
- Cloud Sync (`CLOUDSYNC_S3_BUCKET`)

### 4. Initialize data directories

```bash
./fafb --tui
# → Setup → Initialize directories
```

Or ask Claude after connecting:
```
setup_init_dirs
setup_check
```

### 5. Connect to Claude Code (stdio)

Add to `.mcp.json` in your project root (or `~/.claude/mcp.json` globally):

```json
{
  "mcpServers": {
    "caboose": {
      "command": "/path/to/packages/server/fafb",
      "type": "stdio"
    }
  }
}
```

Claude Code will launch the binary automatically. All 108 tools are available.

### 6. Run as HTTP server (optional)

```bash
./fafb --serve        # all 108 tools on :8080
./fafb --serve-hosted # 88 hosted tools only
./fafb --serve-local  # 20 local tools only
./fafb --serve :9090  # custom port
```

Set `MCP_AUTH_TOKEN` to optionally require bearer auth (recommended for network-exposed instances):
```bash
export MCP_AUTH_TOKEN=$(openssl rand -hex 32)
./fafb --serve
```

### 7. Run with Docker Compose (server + n8n)

```bash
cd /path/to/fafb

cp .env.example .env
# edit .env — fill in secrets

docker compose -f docker/docker-compose.yml up -d
```

**Services:**
| Service | URL | Purpose |
|---------|-----|---------|
| `fafb` | `http://localhost:8080/mcp` | MCP server (all tools) |
| `n8n` | `http://localhost:5678` | Workflow automation |

n8n is pre-wired to call the MCP server at `http://server:8080/mcp`. Import example workflows with `setup_n8n_workflows`.

### 8. Run Slack + Discord bots

```bash
# Both bots concurrently (blocks)
./fafb --bots

# Individual bots
./fafb --slack-bot
./fafb --discord-bot
```

Requires: `ANTHROPIC_API_KEY`, `SLACK_TOKEN` + `SLACK_APP_TOKEN`, `DISCORD_TOKEN`.

---

## Hosted (AWS) Setup

Runs on ECS Fargate. Exposes 88 cloud-safe tools via HTTPS. Slack and Discord bots run as separate ECS tasks.

### Prerequisites

- AWS CLI configured (`aws configure`)
- OpenTofu ≥ 1.6 (`tofu --version`)
- Docker with buildx
- GitHub Actions secrets configured (see [CI/CD secrets](#cicd-secrets))

### 1. Configure Terraform variables

```bash
cd terraform/aws
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars`:

```hcl
aws_region    = "us-east-1"
domain_name   = "mcp.yourdomain.com"
hosted_zone   = "yourdomain.com"
```

### 2. Provision infrastructure

```bash
cd terraform/aws
tofu init
tofu plan
tofu apply
```

This creates:
- ECS Fargate cluster + two services (`fafb-serve`, `fafb-bots`)
- ALB with HTTPS (ACM cert, Route53 DNS)
- ECR repository
- Secrets Manager secret (`fafb/env`)
- S3 bucket (cloud sync)
- CloudWatch log groups

### 3. Push runtime secrets to Secrets Manager

```bash
aws secretsmanager put-secret-value \
  --secret-id fafb/env \
  --secret-string '{
    "ANTHROPIC_API_KEY":  "sk-ant-...",
    "SLACK_TOKEN":        "xoxb-...",
    "SLACK_APP_TOKEN":    "xapp-...",
    "DISCORD_TOKEN":      "...",
    "GITHUB_TOKEN":       "ghp_..."
  }'
```

### 4. Build and push the Docker image

The Pi is ARM64 but Fargate runs AMD64. Use buildx:

```bash
# One-time: create multi-arch builder
docker buildx create --name multiarch --driver docker-container --use

# Build and push AMD64 image to ECR
AWS_ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
AWS_REGION=us-east-1
ECR_REPO=$AWS_ACCOUNT.dkr.ecr.$AWS_REGION.amazonaws.com/fafb

aws ecr get-login-password --region $AWS_REGION | \
  docker login --username AWS --password-stdin $ECR_REPO

docker buildx build \
  --platform linux/amd64 \
  --push \
  -t $ECR_REPO:latest \
  -f docker/Dockerfile .
```

The `deploy-app` and `deploy-bots` CI workflows do this automatically on push to main.

### 5. Force new ECS deployment

```bash
aws ecs update-service \
  --cluster fafb \
  --service fafb-serve \
  --force-new-deployment

aws ecs update-service \
  --cluster fafb \
  --service fafb-bots \
  --force-new-deployment
```

### 6. Connect VS Code

Add to `.vscode/mcp.json`:

```json
{
  "servers": {
    "fafb": {
      "type": "http",
      "url": "https://mcp.yourdomain.com/mcp"
    }
  }
}
```

Or via [Claude Code](https://claude.ai/claude-code) CLI:

```bash
claude mcp add --transport http MCP_CABOOSE https://mcp.yourdomain.com/mcp
```

### 7. Verify

```bash
curl -s https://mcp.yourdomain.com/mcp \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' \
  -H "Content-Type: application/json" | jq '.result.tools | length'
# → 88
```

---

## CI/CD Secrets

Set these in **GitHub → Settings → Secrets → Actions**:

| Secret | Used by |
|--------|---------|
| `AWS_ACCESS_KEY_ID` | deploy-infra, deploy-app, deploy-bots |
| `AWS_SECRET_ACCESS_KEY` | deploy-infra, deploy-app, deploy-bots |
| `SECRET_ANTHROPIC_API_KEY` | deploy-infra (syncs to Secrets Manager) |
| `SECRET_SLACK_TOKEN` | deploy-infra |
| `SECRET_SLACK_APP_TOKEN` | deploy-infra |
| `SECRET_DISCORD_TOKEN` | deploy-infra |
| `SECRET_GITHUB_TOKEN` | deploy-infra |

---

## Google Calendar Setup

1. Create an OAuth2 client in [Google Cloud Console](https://console.cloud.google.com) → APIs & Services → Credentials → Create → OAuth client ID → Desktop app
2. Download `credentials.json` and save to `~/.claude/google/credentials.json`
3. Ask Claude (or run directly): `calendar_auth_url` → open the URL in a browser
4. Copy the `code=` parameter from the redirect URL
5. Run: `calendar_auth_complete` with the code → token saved to `~/.claude/google/calendar-token.json`

---

## JWT RBAC Auth Setup

Per-token access control with magic link exchange. Each JWT carries a tool allowlist and Google OAuth scopes.

### 1. Create a token (CLI or via Claude)

```bash
./fafb auth:create \
  --label "vscode-alice" \
  --tools "calendar_list,calendar_create,note_add,focus_start,focus_status" \
  --google-scopes "calendar" \
  --expires 90
# → Magic link (valid 15 min): http://localhost:8080/auth/verify?token=abc...
```

Or via MCP tool: `auth_create_token(label="vscode-alice", tools="calendar_list,note_add", expires_days=90)`

### 2. Exchange for JWT

```bash
curl "http://localhost:8080/auth/verify?token=<magic>"
# → {"token":"eyJ...","jti":"6ba7b810-...","expires_at":"2026-06-17T00:00:00Z"}
```

### 3. Use JWT as bearer token

```http
Authorization: Bearer eyJ...
```

### 4. Link Discord/Slack/Google identity for SSO

```
auth_link_identity(jti="6ba7b810-...", platform="discord", platform_id="123456789")
auth_link_identity(jti="6ba7b810-...", platform="slack",   platform_id="U0123ABCD")
auth_link_identity(jti="6ba7b810-...", platform="google",  platform_id="alice@gmail.com")
```

Once linked, messages from that Discord/Slack user automatically apply the token's tool ACL and Google scope restrictions.

### 5. Revoke a token

```
auth_revoke_token(jti="6ba7b810-...")
```

### Storage layout

```
~/.claude/auth/
  jwt-secret.key          — 32-byte hex HS256 key (auto-created on first serve)
  issued-tokens.json      — all issued tokens
  magic-tokens.json       — pending 15-min one-time links
  identities.json         — platform:id → JTI mappings

~/.claude/google/
  calendar-token.json           — global/admin Google token
  calendar-token-<jti>.json     — per-user Google token (created after calendar_auth_complete)
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `MCP_AUTH_TOKEN` | Static superuser token (bypasses all ACL) |
| `MCP_BASE_URL` | Base URL used in magic link output (default `http://localhost:8080`) |

---

## AWS Cost Estimates

Running the full fafb stack on AWS ECS Fargate (hosted tier only):

| Service | Spec | Est. monthly cost |
|---------|------|-------------------|
| ECS Fargate — `fafb-serve` | 0.25 vCPU / 0.5 GB | ~$8–12 |
| ECS Fargate — `fafb-bots` | 0.25 vCPU / 0.5 GB | ~$8–12 |
| ALB (Application Load Balancer) | base + ~0.008/LCU-hr | ~$18 |
| ACM (TLS certificate) | managed cert | free |
| ECR (container registry) | 5-image lifecycle policy | ~$0.10/GB/month |
| Secrets Manager | ~3 secrets | ~$1.20 |
| S3 (cloud sync bucket) | small config files | < $0.01 |
| CloudWatch Logs (30-day retention) | low-volume | ~$0.50–2 |
| **Estimated total** | | **~$35–50/month** |

### Budget alert

```bash
aws budgets create-budget \
  --account-id "$(aws sts get-caller-identity --query Account --output text)" \
  --budget '{
    "BudgetName": "fafb-monthly",
    "BudgetLimit": {"Amount": "60", "Unit": "USD"},
    "TimeUnit": "MONTHLY",
    "BudgetType": "COST"
  }' \
  --notifications-with-subscribers '[{
    "Notification": {
      "NotificationType": "ACTUAL",
      "ComparisonOperator": "GREATER_THAN",
      "Threshold": 80
    },
    "Subscribers": [{"SubscriptionType": "EMAIL", "Address": "you@example.com"}]
  }]'
```

---

## Environment Variables Reference

Full reference: [docs/tools.md](tools.md#configuration)

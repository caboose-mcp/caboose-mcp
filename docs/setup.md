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
git clone https://github.com/caboose-mcp/caboose-mcp.git
cd caboose-mcp/packages/server
go build -o caboose-mcp .
```

### 3. Configure with the setup wizard

```bash
./caboose-mcp --setup
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
./caboose-mcp --tui
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
      "command": "/path/to/packages/server/caboose-mcp",
      "type": "stdio"
    }
  }
}
```

Claude Code will launch the binary automatically. All 108 tools are available.

### 6. Run as HTTP server (optional)

```bash
./caboose-mcp --serve        # all 108 tools on :8080
./caboose-mcp --serve-hosted # 88 hosted tools only
./caboose-mcp --serve-local  # 20 local tools only
./caboose-mcp --serve :9090  # custom port
```

Set `MCP_AUTH_TOKEN` to require bearer auth:
```bash
export MCP_AUTH_TOKEN=$(openssl rand -hex 32)
./caboose-mcp --serve
```

### 7. Run with Docker Compose (server + n8n)

```bash
cd /path/to/caboose-mcp

cp .env.example .env
# edit .env — fill in secrets

docker compose -f docker/docker-compose.yml up -d
```

**Services:**
| Service | URL | Purpose |
|---------|-----|---------|
| `caboose-mcp` | `http://localhost:8080/mcp` | MCP server (all tools) |
| `n8n` | `http://localhost:5678` | Workflow automation |

n8n is pre-wired to call the MCP server at `http://server:8080/mcp`. Import example workflows with `setup_n8n_workflows`.

### 8. Run Slack + Discord bots

```bash
# Both bots concurrently (blocks)
./caboose-mcp --bots

# Individual bots
./caboose-mcp --slack-bot
./caboose-mcp --discord-bot
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
- ECS Fargate cluster + two services (`caboose-mcp-serve`, `caboose-mcp-bots`)
- ALB with HTTPS (ACM cert, Route53 DNS)
- ECR repository
- Secrets Manager secret (`caboose-mcp/env`)
- S3 bucket (cloud sync)
- CloudWatch log groups

### 3. Push runtime secrets to Secrets Manager

```bash
aws secretsmanager put-secret-value \
  --secret-id caboose-mcp/env \
  --secret-string '{
    "ANTHROPIC_API_KEY":  "sk-ant-...",
    "MCP_AUTH_TOKEN":     "<openssl rand -hex 32>",
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
ECR_REPO=$AWS_ACCOUNT.dkr.ecr.$AWS_REGION.amazonaws.com/caboose-mcp

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
  --cluster caboose-mcp \
  --service caboose-mcp-serve \
  --force-new-deployment

aws ecs update-service \
  --cluster caboose-mcp \
  --service caboose-mcp-bots \
  --force-new-deployment
```

### 6. Get your bearer token

```bash
aws secretsmanager get-secret-value --secret-id caboose-mcp/env \
  --query 'SecretString' --output text \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['MCP_AUTH_TOKEN'])"
```

### 7. Connect VS Code

Add to `.vscode/mcp.json`:

```json
{
  "servers": {
    "caboose-mcp": {
      "type": "http",
      "url": "https://mcp.yourdomain.com/mcp",
      "headers": { "Authorization": "Bearer <MCP_AUTH_TOKEN>" }
    }
  }
}
```

### 8. Verify

```bash
curl -s -H "Authorization: Bearer <token>" \
  https://mcp.yourdomain.com/mcp \
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
| `SECRET_MCP_AUTH_TOKEN` | deploy-infra |
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

## Environment Variables Reference

Full reference: [docs/tools.md](tools.md#configuration)

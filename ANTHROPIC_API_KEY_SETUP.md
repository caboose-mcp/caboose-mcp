# Setting up ANTHROPIC_API_KEY for fafb

The Discord and Slack bots require an Anthropic API key to interact with Claude. This guide explains how to configure it for local development and CI/CD deployments.

## Table of Contents

1. [Getting an API Key](#getting-an-api-key)
2. [Local Development](#local-development)
3. [CI/CD Pipeline](#cicd-pipeline)
4. [Production Deployment](#production-deployment)
5. [Troubleshooting](#troubleshooting)

---

## Getting an API Key

1. Visit [console.anthropic.com](https://console.anthropic.com/settings/keys)
2. Click **"Create Key"**
3. Give it a name (e.g., "fafb-bots")
4. Copy the key (you won't see it again!)
5. Keep it safe — treat it like a password

The key format looks like: `sk-ant-...` (starts with `sk-ant-`)

---

## Local Development

### Option 1: Using .env file (Recommended)

1. **Copy the example file:**
   ```bash
   cp .env.example .env
   ```

2. **Edit `.env` and add your key:**
   ```bash
   ANTHROPIC_API_KEY=sk-ant-...
   DISCORD_TOKEN=your-discord-token
   DISCORD_BOT_CHANNELS=channel-id-1,channel-id-2
   ```

3. **Run the bot:**
   ```bash
   # Build first
   cd packages/server && go build -o ../../fafb .

   # Run the bot
   ./fafb --bots
   ```

The bot will automatically load the `.env` file at startup.

### Option 2: Environment Variables

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export DISCORD_TOKEN=your-discord-token
cd packages/server && go build -o ../../fafb .
./fafb --bots
```

### Option 3: Docker with .env

```bash
cd packages/server && go build -o ../../fafb .
docker run --env-file .env -v ~/.claude:/root/.claude fafb --bots
```

### Testing Your Setup

Once running, send a message to your bot in Discord or Slack. You should see:
- The bot responds with "⚔️ Response in thread" or similar
- If you get "401 Unauthorized", the API key is invalid

---

## CI/CD Pipeline

### Step 1: Add GitHub Secrets

1. Go to your repository settings: **Settings → Secrets and variables → Actions**
2. Click **"New repository secret"**
3. Add these secrets:

| Secret Name | Value | Notes |
|---|---|---|
| `ANTHROPIC_API_KEY` | `sk-ant-...` | Your Claude API key |
| `DISCORD_TOKEN` | Bot token | Discord bot token |
| `SLACK_TOKEN` | Token | Slack bot token |
| `SLACK_APP_TOKEN` | `xapp-...` | Slack app token |
| `AWS_ACCESS_KEY_ID` | IAM key | Must have `secretsmanager:*` permissions |
| `AWS_SECRET_ACCESS_KEY` | IAM secret | Paired with above |

### Step 2: Populate AWS Secrets Manager

This step syncs your GitHub secrets to AWS so ECS can access them.

**Automatic (via GitHub Actions):**

1. The workflow runs automatically on push to `main` when commit contains `[setup-secrets]`
2. Or manually trigger: **Actions → Setup AWS Secrets → Run workflow**

**Manual (from CLI):**

```bash
# Using environment variables
export ANTHROPIC_API_KEY=sk-ant-...
export DISCORD_TOKEN=...
export SLACK_TOKEN=...
export SLACK_APP_TOKEN=...

./scripts/setup-aws-secrets.sh --from-env
```

**Interactive:**

```bash
./scripts/setup-aws-secrets.sh
# Then enter values when prompted
```

### Step 3: Verify Secrets Are Set

```bash
# Check what's in AWS Secrets Manager
aws secretsmanager get-secret-value \
  --secret-id fafb/env \
  --region us-east-1
```

---

## Production Deployment

### Automatic Deployment Flow

```
Push to main with [setup-secrets]
    ↓
GitHub Actions: setup-secrets.yml runs
    ↓
Secrets synced to AWS Secrets Manager
    ↓
(Optional) ECS service auto-redeployed
    ↓
New task pulls secrets from AWS
    ↓
Bot starts with valid credentials
```

### Manual Deployment

1. **Update AWS secrets:**
   ```bash
   ./scripts/setup-aws-secrets.sh --from-env
   ```

2. **Redeploy the bots service:**
   ```bash
   aws ecs update-service \
     --cluster fafb \
     --service fafb-bots \
     --force-new-deployment
   ```

3. **Watch deployment:**
   ```bash
   aws ecs wait services-stable \
     --cluster fafb \
     --services fafb-bots
   ```

4. **Check logs:**
   ```bash
   aws logs tail /ecs/fafb/bots --follow
   ```

---

## Troubleshooting

### Error: "ANTHROPIC_API_KEY is not set"

**Cause:** The environment variable isn't loaded.

**Fix:**
- Local: Ensure `.env` file exists and is in the working directory
- Docker: Pass `--env-file .env` or use environment variables
- ECS: Verify AWS Secrets Manager has the key (see Verify Secrets above)

### Error: "401 Unauthorized — invalid x-api-key"

**Cause:** The API key is invalid or expired.

**Fix:**
- Get a new key from [console.anthropic.com](https://console.anthropic.com/settings/keys)
- Update GitHub secrets with the new key
- Run `./scripts/setup-aws-secrets.sh --from-env` to sync to AWS
- Redeploy: `aws ecs update-service --cluster fafb --service fafb-bots --force-new-deployment`

### Error: "AWS credentials are not configured"

**Cause:** AWS CLI can't find your credentials.

**Fix:**
```bash
aws configure
# Or set environment variables
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
```

### Bot doesn't respond to messages

**Check:**
1. Bot is running: `docker ps | grep fafb` or `ps aux | grep fafb`
2. Logs show no errors: `docker logs container-name` or `aws logs tail /ecs/fafb/bots`
3. API key is valid: `aws secretsmanager get-secret-value --secret-id fafb/env`
4. Bot can reach Discord/Slack APIs (firewall/proxy issues?)

---

## Architecture Overview

```
Local Development:
  .env file
    ↓
  godotenv.Load()
    ↓
  config.Load() → cfg.AnthropicAPIKey
    ↓
  RunBotAgent() → anthropic.NewClient(option.WithAPIKey(...))
    ↓
  Discord/Slack bot responds

Production (ECS):
  GitHub Secrets
    ↓
  GitHub Actions workflow
    ↓
  AWS Secrets Manager (fafb/env)
    ↓
  ECS Task Definition (secrets section)
    ↓
  Container environment variables
    ↓
  config.Load() → cfg.AnthropicAPIKey
    ↓
  RunBotAgent() → anthropic.NewClient(option.WithAPIKey(...))
    ↓
  Discord/Slack bot responds
```

---

## Security Best Practices

1. **Never commit secrets to Git**
   - `.env` is in `.gitignore` — don't change this!
   - Use GitHub Secrets for CI/CD

2. **Rotate keys regularly**
   - Generate new keys every 90 days
   - Old keys can't be recovered, only replaced

3. **Use least privilege**
   - Limit API key usage to only what's needed
   - Consider rate limits per key

4. **Monitor API usage**
   - Check [console.anthropic.com](https://console.anthropic.com/account) for usage
   - Set spending limits if available

---

## Quick Reference

| Scenario | Command |
|---|---|
| Run bot locally | `./fafb --bots` |
| Build bot | `cd packages/server && go build -o ../../fafb .` |
| Update AWS secrets | `./scripts/setup-aws-secrets.sh --from-env` |
| Check AWS secrets | `aws secretsmanager get-secret-value --secret-id fafb/env` |
| Redeploy ECS service | `aws ecs update-service --cluster fafb --service fafb-bots --force-new-deployment` |
| View logs | `aws logs tail /ecs/fafb/bots --follow` |

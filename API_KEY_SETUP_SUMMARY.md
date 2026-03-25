# API Key Setup Summary

## Problem
The Discord and Slack bots were getting `401 Unauthorized` errors when trying to call the Claude API because the `ANTHROPIC_API_KEY` wasn't being passed to the SDK client.

The fix was already in the codebase (commit 83dbbf9), but documentation and automation were missing.

## Solution Overview

### For Local Development

```bash
# 1. Copy the template
cp .env.example .env

# 2. Add your Anthropic API key (get from https://console.anthropic.com/settings/keys)
# Edit .env and set:
ANTHROPIC_API_KEY=sk-ant-...
DISCORD_TOKEN=your-token
DISCORD_BOT_CHANNELS=channel-id

# 3. Run the bot
cd packages/server && go build -o ../../fafb .
./fafb --bots
```

### For CI/CD & Production

```bash
# 1. Add GitHub Secrets (Settings → Secrets and variables → Actions)
#    - ANTHROPIC_API_KEY (from console.anthropic.com)
#    - DISCORD_TOKEN
#    - SLACK_TOKEN
#    - SLACK_APP_TOKEN
#    - AWS_ACCESS_KEY_ID
#    - AWS_SECRET_ACCESS_KEY

# 2. Option A: Use GitHub Actions (automatic)
#    Commit with [setup-secrets] in message to trigger workflow:
git commit -m "chore: Update API keys [setup-secrets]"
git push

# 2. Option B: Manual CLI setup
export ANTHROPIC_API_KEY=sk-ant-...
export DISCORD_TOKEN=...
export SLACK_TOKEN=...
export SLACK_APP_TOKEN=...

./scripts/setup-aws-secrets.sh --from-env

# 3. Verify secrets were synced
aws secretsmanager get-secret-value --secret-id fafb/env

# 4. Redeploy bots service
aws ecs update-service \
  --cluster fafb \
  --service fafb-bots \
  --force-new-deployment
```

## Files Added/Modified

### New Files
1. **`ANTHROPIC_API_KEY_SETUP.md`** — Comprehensive setup guide with troubleshooting
2. **`scripts/setup-aws-secrets.sh`** — Automation script for syncing secrets to AWS
3. **`.github/workflows/setup-secrets.yml`** — GitHub Actions workflow for CI/CD

### Modified Files
1. **`.env.example`** — Added `ANTHROPIC_API_KEY`, `DISCORD_BOT_CHANNELS`, `SLACK_BOT_CHANNELS`, and ElevenLabs config

## Architecture

```
┌─ Local Development ─────────────────┐
│                                     │
│  .env                               │
│   ↓                                 │
│  godotenv.Load()                    │
│   ↓                                 │
│  config.Load() →                    │
│  cfg.AnthropicAPIKey                │
│   ↓                                 │
│  RunBotAgent()                      │
│   ↓                                 │
│  anthropic.NewClient(               │
│    option.WithAPIKey(...)           │
│  )                                  │
│   ↓                                 │
│  ✓ Bot responds                     │
└─────────────────────────────────────┘

┌─ CI/CD & Production ────────────────┐
│                                     │
│  GitHub Secrets                     │
│   ↓                                 │
│  GitHub Actions Workflow            │
│   ↓                                 │
│  AWS Secrets Manager                │
│  (fafb/env secret)                  │
│   ↓                                 │
│  ECS Task Definition                │
│  (secrets: [])                      │
│   ↓                                 │
│  Container Env Vars                 │
│   ↓                                 │
│  config.Load() →                    │
│  cfg.AnthropicAPIKey                │
│   ↓                                 │
│  RunBotAgent()                      │
│   ↓                                 │
│  ✓ Bot responds                     │
└─────────────────────────────────────┘
```

## Key Changes in Code

The bot was already fixed in commit 83dbbf9 to pass the API key explicitly:

```go
// packages/server/tools/bot_agent.go line 83-85
client := anthropic.NewClient(
    option.WithAPIKey(cfg.AnthropicAPIKey),
)
```

This ensures the SDK uses the configured API key instead of looking for it elsewhere.

## Verification

### Local
```bash
# Bot should respond to Discord/Slack messages
# Check logs: no "401 Unauthorized" errors
```

### Production (ECS)
```bash
# Check that secrets exist
aws secretsmanager get-secret-value --secret-id fafb/env

# Check ECS logs
aws logs tail /ecs/fafb/bots --follow

# Verify bot is responding to messages
```

## Troubleshooting Quick Links

| Issue | Solution |
|-------|----------|
| "ANTHROPIC_API_KEY is not set" | See `.env.example` or [ANTHROPIC_API_KEY_SETUP.md](ANTHROPIC_API_KEY_SETUP.md#error-anthropic_api_key-is-not-set) |
| "401 Unauthorized" | Key is invalid/expired. Get new one from console.anthropic.com |
| AWS secrets not syncing | Run `./scripts/setup-aws-secrets.sh --from-env` or check GitHub Actions |
| Bot not responding | Check ECS logs: `aws logs tail /ecs/fafb/bots --follow` |

## Next Steps

1. **Add GitHub Secrets** (if using CI/CD)
   - Go to repo Settings → Secrets and variables → Actions
   - Add `ANTHROPIC_API_KEY`, `DISCORD_TOKEN`, `SLACK_TOKEN`, `SLACK_APP_TOKEN`

2. **Test Locally**
   ```bash
   cp .env.example .env
   # Edit .env with your keys
   cd packages/server && go build -o ../../fafb .
   ANTHROPIC_API_KEY=sk-ant-... ./fafb --bots
   ```

3. **Deploy to Production**
   ```bash
   ./scripts/setup-aws-secrets.sh --from-env
   aws ecs update-service --cluster fafb --service fafb-bots --force-new-deployment
   ```

---

For more detailed information, see [ANTHROPIC_API_KEY_SETUP.md](ANTHROPIC_API_KEY_SETUP.md)

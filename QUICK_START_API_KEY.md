# Quick Start: Setting Up ANTHROPIC_API_KEY

## 60-Second Setup (Local)

```bash
# 1. Get API key from https://console.anthropic.com/settings/keys

# 2. Create .env file
cp .env.example .env

# 3. Edit .env and add (using your actual key):
echo "ANTHROPIC_API_KEY=sk-ant-YOUR_KEY_HERE" >> .env
echo "DISCORD_TOKEN=your-discord-token" >> .env
echo "DISCORD_BOT_CHANNELS=your-channel-id" >> .env

# 4. Build and run
cd packages/server && go build -o ../../fafb .
./fafb --bots
```

## 60-Second Setup (CI/CD)

```bash
# 1. Go to repo → Settings → Secrets and variables → Actions
# 2. Click "New repository secret"
# 3. Add these secrets:
#    - ANTHROPIC_API_KEY (from console.anthropic.com)
#    - DISCORD_TOKEN
#    - SLACK_TOKEN
#    - SLACK_APP_TOKEN
#    - AWS_ACCESS_KEY_ID
#    - AWS_SECRET_ACCESS_KEY

# 4. Sync to AWS (from your local machine with AWS CLI configured)
export ANTHROPIC_API_KEY=sk-ant-...
export DISCORD_TOKEN=...
export SLACK_TOKEN=...
export SLACK_APP_TOKEN=...

./scripts/setup-aws-secrets.sh --from-env

# 5. Redeploy
aws ecs update-service --cluster fafb --service fafb-bots --force-new-deployment
```

## Environment Variables

| Variable | Where to Get | Used In |
|----------|---|---|
| `ANTHROPIC_API_KEY` | [console.anthropic.com](https://console.anthropic.com/settings/keys) | Discord/Slack bot AI responses |
| `DISCORD_TOKEN` | Discord Developer Portal | Discord bot connection |
| `SLACK_TOKEN` | Slack App settings | Slack bot connection |
| `SLACK_APP_TOKEN` | Slack App settings (starts with `xapp-`) | Slack Socket Mode |

## Common Commands

```bash
# Test API key locally
echo "ANTHROPIC_API_KEY=sk-ant-..." > /tmp/test.env
source /tmp/test.env
./fafb --bots

# Check AWS secrets
aws secretsmanager get-secret-value --secret-id fafb/env

# View bot logs (production)
aws logs tail /ecs/fafb/bots --follow

# Redeploy bots service
aws ecs update-service --cluster fafb --service fafb-bots --force-new-deployment
```

## Troubleshooting

| Error | Fix |
|-------|-----|
| "ANTHROPIC_API_KEY is not set" | Check `.env` file exists in working directory |
| "401 Unauthorized" | Key is invalid. Get new one from console.anthropic.com |
| Bot doesn't respond | Check logs: `./fafb --bots 2>&1 \| grep -i error` |

## More Info

- Detailed guide: [ANTHROPIC_API_KEY_SETUP.md](ANTHROPIC_API_KEY_SETUP.md)
- Summary: [API_KEY_SETUP_SUMMARY.md](API_KEY_SETUP_SUMMARY.md)

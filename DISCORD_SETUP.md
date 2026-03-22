# Discord Bot Setup for org_health & org_manager

## Step 1: Create Discord Bot (5 minutes)

1. Go to https://discord.com/developers/applications
2. Click **"New Application"** → Name it `fafb`
3. Go to **"Bot"** tab → Click **"Add Bot"**
4. Under **TOKEN**, click **"Copy"** → Save this somewhere safe (you'll need it)
5. Scroll down to **"MESSAGE CONTENT INTENT"** → Toggle **ON**
6. Scroll down to **"SERVER MEMBERS INTENT"** → Toggle **ON** (optional but recommended)

**Your Discord Bot Token:**
```
(you'll paste it here)
```

---

## Step 2: Invite Bot to Your Server

1. In the same app, go to **OAuth2** → **URL Generator**
2. Select scopes:
   - ✅ `bot`
   - ✅ `applications.commands`
3. Select permissions:
   - ✅ Send Messages
   - ✅ Read Messages/View Channels
   - ✅ Read Message History
4. Copy the generated URL and open it in browser
5. Select your Discord server and authorize

---

## Step 3: Get Channel ID

1. In Discord, enable **Developer Mode**: Settings → Advanced → Developer Mode → ON
2. Right-click on your channel (e.g., #dev-tools)
3. Click **"Copy Channel ID"**

**Your Channel ID:**
```
(you'll paste it here)
```

---

## Step 4: Create .env File

Create `/home/caboose/dev/fafb/.env`:

```bash
# GitHub Orgs
GITHUB_ORGS=caboose-mcp

# Discord Bot
DISCORD_TOKEN=your_bot_token_here
DISCORD_BOT_CHANNELS=your_channel_id_here

# Optional: Webhook for alerts
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
```

---

## Step 5: Start the Bot

```bash
cd /home/caboose/dev/fafb/packages/server
export $(cat ../.env | xargs)  # Load env vars
./fafb --bots
```

You should see:
```
2026-03-22 15:30:45 starting discord bot
2026-03-22 15:30:46 Discord bot ready!
```

---

## Step 6: Test It!

In your Discord channel:
```
@fafb org_health_status
```

Bot should reply:
```
=== Organization Health Status ===

Last refreshed: 2026-03-22 15:35:12
Organizations: caboose-mcp
Total repos scanned: 5

Open PRs:  7
Failing CI: 0
Copilot blocks: 0
```

---

## Troubleshooting

**Bot not responding?**
- Check bot is in the channel (should see it in member list)
- Check MESSAGE CONTENT INTENT is ON in Developer Portal
- Check DISCORD_BOT_CHANNELS has correct channel ID
- Check bot token is correct in .env

**"Cannot find module"?**
- Make sure you're in `/packages/server` directory
- Run `go build -o fafb .` first

**Permission denied?**
- Make sure bot has "Send Messages" permission in the channel

---

## Next Steps

Once bot is working, try:
```
@fafb org_health_refresh
@fafb org_health_next_pr
@fafb org_sync_status /home/caboose/dev
@fafb org_pull_all /home/caboose/dev --dry_run
```

---

## Keep Bot Running

### Option 1: systemd (recommended)

Create `/etc/systemd/user/fafb-bot.service`:
```ini
[Unit]
Description=fafb Discord Bot
After=network.target

[Service]
Type=simple
WorkingDirectory=/home/caboose/dev/fafb/packages/server
Environment="$(cat /home/caboose/dev/fafb/.env | xargs)"
ExecStart=/home/caboose/dev/fafb/packages/server/fafb --bots
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
```

Then:
```bash
systemctl --user enable fafb-bot
systemctl --user start fafb-bot
systemctl --user status fafb-bot
```

### Option 2: tmux (simple)

```bash
tmux new-session -d -s fafb-bot -c /home/caboose/dev/fafb/packages/server \
  "export \$(cat ../.env | xargs) && ./fafb --bots"
```

### Option 3: nohup (simplest)

```bash
cd /home/caboose/dev/fafb/packages/server
nohup bash -c 'export $(cat ../.env | xargs) && ./fafb --bots' > fafb-bot.log 2>&1 &
```

---

## Security Notes

⚠️ **DO NOT commit .env to git!**
```bash
echo ".env" >> .gitignore
git add .gitignore
```

⚠️ **Keep your Discord bot token secret** - treat it like a password

⚠️ **Limit channel access** - only invite the bot to channels where you want it active

---

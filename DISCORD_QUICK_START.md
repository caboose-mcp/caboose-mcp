# Discord Bot Quick Start

## TL;DR - 2 Minute Setup

### 1. Create Discord Bot
Go to https://discord.com/developers/applications
- Click "New Application" → name: `fafb`
- Bot tab → "Add Bot"
- Copy TOKEN
- Toggle "MESSAGE CONTENT INTENT" to ON
- Save token somewhere

### 2. Run Setup Script
```bash
cd /home/caboose/dev/fafb
bash scripts/setup-discord-bot.sh
```

It will ask for:
- Discord bot token (paste what you copied)
- Discord channel ID (right-click channel, "Copy Channel ID")
- GitHub orgs to monitor (default: caboose-mcp)

### 3. Start Bot
```bash
cd packages/server
export $(cat ../.env | xargs)
./fafb --bots
```

### 4. Test
In Discord channel:
```
@fafb org_health_status
```

---

## Essential Commands

```
@fafb org_health_refresh          # Refresh org health cache
@fafb org_health_status           # Show org status
@fafb org_health_next_pr          # Show next PR to work on
@fafb org_sync_status /home/caboose/dev         # Check repo sync state
@fafb org_pull_all /home/caboose/dev --dry_run  # Preview what would pull
@fafb org_pull_all /home/caboose/dev --stash    # Pull all repos
@fafb org_branch_cleanup /home/caboose/dev      # List stale branches
```

---

## Files Created

- `/home/caboose/dev/fafb/.env` — Bot credentials
- `/home/caboose/dev/fafb/DISCORD_SETUP.md` — Full setup guide
- `/home/caboose/dev/fafb/scripts/setup-discord-bot.sh` — Automated setup

---

## Keep Bot Running

**Simple (nohup):**
```bash
cd /home/caboose/dev/fafb/packages/server
nohup bash -c 'export $(cat ../.env | xargs) && ./fafb --bots' > bot.log 2>&1 &
```

**Persistent (systemd):**
See `DISCORD_SETUP.md` for systemd service setup

---

## Troubleshooting

**Bot not responding?**
- Check bot is in channel (right-click channel → Members)
- Check MESSAGE CONTENT INTENT is ON in Developer Portal
- Check .env file has correct token and channel ID
- Check bot process is running: `ps aux | grep fafb`

**Build fails?**
```bash
cd packages/server
go build -o fafb .
```

**Permission denied?**
Make sure bot has "Send Messages" permission in the channel

---

## Next: Use with Your Team

Share the Discord channel link so your team can see:
- Org health status
- Next PRs to work on
- Which repos need syncing
- CI failures in real-time

Just type: `@fafb org_health_status` and the whole team sees your org's status! 🚀

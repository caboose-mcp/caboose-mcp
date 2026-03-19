# Plan: Google Messages Integration

**Status:** Design only — not yet implemented.

---

## Overview

Add a Google Messages chat surface alongside Discord and Slack, allowing the Caboose bot to receive and respond to SMS/RCS messages via Google's Business Messages API (or Android Messages for Web integration).

---

## Options

### Option A: Google Business Messages (recommended for production)

Google's Business Messages API enables RCS/SMS at scale via a verified business agent. Requires Google verification.

**How it works:**
1. Register a Business Messages agent at [business.google.com/messages](https://business.google.com/messages)
2. Google provides a webhook endpoint to receive inbound messages
3. Send replies via the Business Messages REST API (`POST /v1/conversations/{id}/messages`)
4. Requires domain verification and Google approval (~2–4 weeks)

**Limitations:** Requires business verification; not suitable for personal/prototype use.

### Option B: Android Messages for Web + Puppeteer bridge (personal use)

A lightweight bridge that monitors `messages.google.com` (the web client) using a headless browser session and relays messages to/from the bot.

**How it works:**
1. Authenticate once via QR code scan on `messages.google.com`
2. A long-running service polls or watches for new messages via DOM mutation observer
3. Incoming messages forwarded to `RunBotAgent` with key `"google-messages:<thread_id>"`
4. Bot reply sent back via the web client automation

**Limitations:** Fragile (web UI changes break it), not officially supported, no multi-device pairing.

---

## Recommended Implementation (Option B — personal/prototype)

### New file: `packages/server/tools/messages_gateway.go`

```go
// RunGoogleMessagesBot bridges Android Messages for Web → Caboose bot agent.
// Uses a Playwright/Chromium session to read/send messages.
func RunGoogleMessagesBot(cfg *config.Config) error
```

### Dependencies

- `github.com/playwright-community/playwright-go` — Chromium automation
- Session stored at `~/.claude/google/messages-session/` (persistent browser profile)

### New CLI flag

```bash
./caboose-mcp --messages-bot
# → launches Chromium, shows QR if not authenticated, then polls
```

### Identity integration

Messages from phone number `+1-555-0100` map to identity key `google-messages:+15550100`, which can be linked via:

```
auth_link_identity(jti="...", platform="google-messages", platform_id="+15550100")
```

### Flow

```
Incoming SMS/RCS
  → messages.google.com (web client)
    → Playwright DOM watcher
      → RunBotAgent(ctx, cfg, provider, "google-messages:+15550100", text)
        → Claude agent loop with mobile tool tier
          → reply sent back via Playwright .click() + .type()
```

---

## Architecture Changes Required

### `tools/messages_gateway.go` (new)
- `RunGoogleMessagesBot(cfg)` — main loop
- `GoogleMessagesChatProvider` implementing `ChatProvider` interface
- Playwright session management + QR auth flow

### `main.go`
- Add `--messages-bot` flag (alongside `--slack-bot`, `--discord-bot`)
- Update `--bots` to optionally include messages bot if `GOOGLE_MESSAGES_ENABLED=true`

### `tools/bot_agent.go`
- No changes needed — `RunBotAgent` already handles any `ChatProvider`

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GOOGLE_MESSAGES_ENABLED` | Set to `true` to enable messages bot in `--bots` mode |
| `GOOGLE_MESSAGES_SESSION_DIR` | Path to Playwright session (default `~/.claude/google/messages-session`) |
| `GOOGLE_MESSAGES_PHONE` | Expected phone number to receive from (optional filter) |

---

## Storage

```
~/.claude/google/messages-session/    — Playwright persistent browser profile (QR auth)
~/.claude/bot-memory/google-messages:<thread>.json  — per-thread conversation history
```

---

## Effort Estimate

| Task | Effort |
|------|--------|
| Playwright session + QR auth flow | Medium (2–3 days) |
| DOM watcher + message parser | Medium (1–2 days) |
| Reply automation | Small (1 day) |
| Identity integration + ACL | Trivial (already done) |
| `--messages-bot` CLI flag | Trivial |
| Testing (fragile by nature) | Medium |

**Total:** ~1 week for a working prototype. Fragility of web scraping means ongoing maintenance is expected when Google updates the Messages web UI.

---

## Alternative: Google Chat (Workspace)

If the use case is team/workspace messaging rather than SMS, Google Chat has an official bot API with webhooks and no web scraping. Requires Google Workspace account. Would follow the same pattern as the Discord/Slack gateway — far more reliable than Option B above.

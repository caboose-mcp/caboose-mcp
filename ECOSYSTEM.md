# fafb Ecosystem Manager

> Secure, integrated toolkit for managing 130+ MCP tools with Discord OAuth, auto-deployment, and admin approval workflows.

## Overview

fafb (formerly caboose-mcp) is a personal AI toolserver providing 130+ MCP tools across three tiers (hosted, local, common). The Ecosystem Manager implements a complete workflow for authentication, tool creation, testing, and deployment.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Users (Browser + Claude Code)                              │
│ • Discord OAuth login → JWT token                          │
│ • Web UI: Create, test, approve tools                      │
│ • CLI: MCP tools (repo_*, gamma_*)                         │
└──────────────────┬──────────────────────────────────────────┘
                   │ HTTPS + JWT/Bearer token
                   ▼
┌─────────────────────────────────────────────────────────────┐
│ fafb Server (Go) — 130 Tools                               │
│ ├─ Auth Layer (OAuth + JWT RBAC)                          │
│ ├─ Tool Registration (hosted/local/common)                │
│ ├─ Repo Tools (create, test, approve, deploy)            │
│ └─ Gamma Integration (auto-generate presentations)        │
└──────────────────┬──────────────────────────────────────────┘
                   │
                   ▼
     ┌─────────────────────────────────┐
     │ Storage (~/.claude/)            │
     │ ├─ auth/jwt-secret.key         │
     │ ├─ pending-tools/*.json        │
     │ └─ audit/audit.log             │
     └─────────────────────────────────┘
```

## Authentication

### Discord OAuth2

Secure OAuth2 PKCE flow for Discord user authentication:

```bash
# Step 1: User clicks "Login with Discord" button
# UI redirects to: /auth/discord/start?redirect_uri=<UI_ORIGIN>/auth/callback

# Step 2: User approves Discord consent screen
# Discord redirects to: /auth/discord/callback?code=<AUTH_CODE>&state=<STATE>

# Step 3: Server exchanges code for token
# Returns: JWT token with discord_id in claims, stored in localStorage

# Step 4: All API calls use Bearer <JWT> token
# JWT includes: scopes (repo, repo:admin), tool ACLs, user identity
```

**Configuration:**
```env
DISCORD_OAUTH_CLIENT_ID=your_client_id
DISCORD_OAUTH_CLIENT_SECRET=your_client_secret
DISCORD_OAUTH_REDIRECT_URI=https://ui.mcp.chrismarasco.io/auth/callback
```

### JWT + Magic Link

Existing magic link authentication for CLI users:

```bash
# Create a token that anyone can use
fafb auth:create --label "my-token" --tools "note_add,focus_start"

# Output includes magic link:
# https://mcp.chrismarasco.io/auth/verify?token=<MAGIC_TOKEN>

# Exchange magic link for JWT (15-min window)
curl https://mcp.chrismarasco.io/auth/verify?token=<MAGIC_TOKEN>

# Response: {"token":"eyJ...","jti":"...","expires_at":"..."}
```

## Tool Creation Workflow

### Phase 1: Draft

**Via Web UI:**
1. Navigate to `/admin/create-tool`
2. Fill in tool details (name, description, category, tier, tags)
3. Add parameters using form builder
4. Click "Create Tool"
5. Tool saved to `~/.claude/pending-tools/<tool_name>.json`

**Via MCP:**
```bash
fafb repo_create_tool \
  --name "my_tool" \
  --description "What it does" \
  --category "dev" \
  --tier "hosted" \
  --parameters_json '[{"name":"input","type":"string","required":true,"description":"Input text"}]'
```

### Phase 2: Test

**Via Web UI:**
1. Navigate to `/admin/pending-tools`
2. Click "Test" on the tool
3. Fill in test parameters
4. Run test and see validation results

**Via MCP:**
```bash
fafb repo_test_tool \
  --tool_name "my_tool" \
  --test_input_json '{"input":"hello"}'
```

**Test Results:**
- ✅ Parameters validated (count & types)
- ❌ Validation errors (missing required, type mismatch)
- Status: Ready for deployment

### Phase 3: Approve

**Via Web UI:**
1. Navigate to `/admin/pending-tools`
2. Review tool code and test results
3. Click "Approve" with optional notes
4. Tool commits to caboose-mcp/main
5. CI runs, auto-deploy if passing

**Via MCP:**
```bash
fafb repo_approve_tool \
  --tool_name "my_tool" \
  --approver_notes "Looks good!"
```

**What happens:**
1. Tool committed to `packages/server/tools/my_tool.go`
2. Updated in `main.go` tool registration
3. Pushed to main branch
4. GitHub Actions CI runs (lint, test, build, e2e)
5. If CI passes, auto-merge + auto-deploy
6. UI auto-syncs 130 tools on next update

### Phase 4: Reject

**Via Web UI:**
1. Navigate to `/admin/pending-tools`
2. Click "Reject"
3. Confirm rejection

**Via MCP:**
```bash
fafb repo_reject_tool \
  --tool_name "my_tool" \
  --reason "Does not meet requirements"
```

**What happens:**
1. Pending tool deleted from `~/.claude/pending-tools/`
2. Draft is discarded
3. Creator notified (future: Discord DM, email)

## Tool Statistics

### Current Tools: 130

| Tier | Count | Examples |
|------|-------|----------|
| **Hosted** | 68 | calendar, github, slack, discord, notes, focus, learning, docker, database |
| **Local** | 25 | docker_exec, bambu_print, chezmoi, blender, execute_command |
| **Common** | 3 | jokes, claude_files, setup |

### By Category

- Auth & RBAC (7) — JWT, Discord OAuth, magic links
- GitHub (8) — repos, PRs, issues, search
- Slack (6) — messages, channels, reactions
- Discord (5) — bots, DMs, server info
- Calendar (3) — events, availability
- Notes (4) — create, list, search, backup
- Focus (5) — sessions, timers, config
- Learning (5) — language sessions, exercises
- Sources (6) — RSS, monitoring, digests
- Docker (6) — containers, images, logs
- Database (4) — query, backup, restore
- *And 70+ more across system, dev, integration, automation categories*

## Security & Scopes

### Token Scopes

```
"repo" — Tool creation & testing (limited)
├─ repo_create_tool
├─ repo_test_tool
├─ repo_list_pending_tools
└─ repo_sync_ui

"repo:admin" — Deployment & management (admin-only)
├─ repo_approve_tool
├─ repo_reject_tool
└─ repo_deploy
```

### Permission Model

```
Unauthenticated:
└─ GET /tools — Browse public tool catalog
└─ GET /health — Server health check

User (JWT with "repo" scope):
└─ POST /api/repo_create_tool — Draft new tools
└─ POST /api/repo_test_tool — Test drafts
└─ GET /api/repo_list_pending_tools — View pending

Admin (JWT with "repo:admin" scope):
└─ POST /api/repo_approve_tool — Approve for deployment
└─ POST /api/repo_reject_tool — Discard drafts
└─ POST /api/repo_deploy — Trigger deployment

Super-admin (MCP_AUTH_TOKEN env var):
└─ All endpoints + all scopes
```

## Deployment Pipeline

### Automatic Deployment

```
Developer commits tool to caboose-mcp/main
  ↓
GitHub Actions: ci.yml (lint, test, build, e2e)
  ↓ (all checks pass)
Auto-merge: ui/.github/workflows/auto-merge-tool-sync.yml
  ↓
GitHub Actions: deploy-app.yml
  ├─ Build Docker image
  ├─ Push to AWS ECR
  ├─ Redeploy ECS task (fafb-serve)
  └─ Index documentation (Greptile)
  ↓
Live! Tool available in MCP server
```

### Manual Deployment

```bash
# Trigger via MCP
fafb repo_deploy --service "hosted"

# Or via GitHub CLI
gh workflow run deploy-app.yml \
  -R caboose-mcp/caboose-mcp \
  -f service=hosted
```

## Tool Sync Automation

### How It Works

1. **Detection:** Tool files changed in `packages/server/tools/**`
2. **Extraction:** Go AST parser extracts 130 tool definitions
3. **Sync:** UI sync script updates `src/data/tools.ts`
4. **PR:** Create PR to UI repo with auto-labels
5. **CI:** Run tests on PR (lint, build, type-check)
6. **Merge:** Auto-merge if CI passes
7. **Deploy:** UI deployed to S3 + CloudFront

**Manual Trigger:**
```bash
# Extract tools from Go source
fafb repo_sync_ui

# Or directly:
cd packages/server/tools
go run extract-tools.go > /tmp/tools.json
cd /home/caboose/dev/ui
node scripts/sync-tools-from-mcp.cjs /tmp/tools.json
```

## Gamma Presentations

Auto-generate presentation decks for announcements and team communication.

```bash
# Create presentation with configurable sections
fafb gamma_generate_deck \
  --title "fafb v2 Release" \
  --sections "overview,tools,benefits,setup" \
  --theme "dark" \
  --tools_json "all"

# Output:
# ✅ Presentation generated!
# Title: fafb v2 Release
# Slides: 8
# URL: https://gamma.app/share/deck_1710962400
# Theme: dark
```

**Sections Available:**
- `overview` — What is fafb?
- `tools` — 130+ tools showcase
- `benefits` — Why use fafb
- `setup` — Quick start guide
- `architecture` — System design

## API Endpoints

### Public

```
GET /health                              — Server health check
GET /api/stats                           — Tool statistics
GET /auth/discord/start?redirect_uri=    — Start Discord OAuth flow
```

### Authenticated (Bearer JWT)

```
POST /api/repo_create_tool               — Create tool draft
POST /api/repo_test_tool                 — Test pending tool
GET  /api/repo_list_pending_tools        — List pending tools
POST /api/repo_sync_ui                   — Manually sync UI
```

### Admin (Bearer JWT with repo:admin scope)

```
POST /api/repo_approve_tool              — Approve and deploy
POST /api/repo_reject_tool               — Discard tool
POST /api/repo_deploy                    — Trigger deployment
```

## Notifications

### Discord

When a tool is approved, optionally send notification to Discord:

```env
DISCORD_WEBHOOK=https://discordapp.com/api/webhooks/...
```

Completion notification:
```bash
gh workflow run notify-phase-completion.yml -R caboose-mcp/caboose-mcp
```

### Email

For deployment notifications:

```env
EMAIL_USERNAME=your-email@gmail.com
EMAIL_PASSWORD=your-app-password
EMAIL_RECIPIENT=cxm6467@gmail.com
```

## Development

### Running Locally

```bash
# Start fafb server (all tools)
cd packages/server
go build -o fafb .
./fafb

# Or HTTP server
./fafb --serve

# Or with terminal UI
./fafb --tui

# In another terminal, start UI dev server
cd packages/ui
pnpm dev

# Navigate to http://localhost:5173
```

### Testing

```bash
# Server tests
cd packages/server
go test ./tools/... -v -race

# UI tests
cd packages/ui
pnpm test

# E2E tests (CI runs these)
./fafb auth:create --label "test" --expires 1
```

## Roadmap

### ✅ Completed

- Phase 1: Discord OAuth Integration
- Phase 2: Auto-Documentation & Auto-Deployment (130 tools extracted)
- Phase 3: Repository Management Tools (repo_*, sandbox testing)
- Phase 4: Gamma Presentation Integration
- Phase 5: GitHub PR-Based Tool Submission
- Phase 6: Admin UI forms + sandbox validation

### 🔄 In Progress

- Real Gamma API integration (currently simulated)
- Full sandbox execution (Go plugin system)
- Performance optimizations

### 🚀 Future

- Tool versioning + rollback
- Multi-tenant support
- Tool marketplace
- Auto-scaling for hosted tools
- Real-time collaboration
- GraphQL API

## Troubleshooting

### Tool creation failed

**Check:**
1. JWT token is valid (not expired)
2. Token has "repo" scope
3. Tool name is snake_case, lowercase only
4. Description is not empty

### Test validation errors

**Common issues:**
- Missing required parameters (add to test input)
- Type mismatch (string vs number vs boolean)
- Invalid parameter name (must match definition exactly)

### Deployment stuck

**Check:**
1. CI passed on the PR
2. No conflicts in main branch
3. GitHub Actions not rate-limited
4. AWS ECS task is available

## Support

- **Issues:** https://github.com/caboose-mcp/caboose-mcp/issues
- **Discussions:** https://github.com/caboose-mcp/caboose-mcp/discussions
- **Live Server:** https://mcp.chrismarasco.io/
- **Web UI:** https://mcp.chrismarasco.io/ui/

## License

MIT — See [LICENSE](LICENSE)

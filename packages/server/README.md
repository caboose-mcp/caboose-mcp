# caboose-mcp

A personal MCP (Model Context Protocol) server written in Go. Runs as a stdio process alongside Claude, exposing 105 tools across 25 feature groups — from source monitoring and self-improvement suggestions to 3D printing, focus sessions, and n8n workflow automation.

---

## Table of Contents

- [VS Code Extension](#vs-code-extension)
- [Requirements](#requirements)
- [Installation](#installation)
- [First-time Setup](#first-time-setup)
- [Usage Modes](#usage-modes)
- [Configuration Reference](#configuration-reference)
- [Tool Reference](#tool-reference)
- [n8n Integration](#n8n-integration)
- [Architecture](#architecture)

---

## VS Code Extension

A companion VS Code extension is available at [caboose-mcp/vscode-extension](https://github.com/caboose-mcp/vscode-extension). It connects to this server over stdio or HTTP and provides:

- Sidebar panel with all tools grouped by category
- One-click tool execution with parameter prompts
- Status bar widget polling any tool on an interval (defaults to `focus_status`)

---

## Requirements

- **Go 1.24+** — `go build` only, no runtime dependencies
- **git**, **gh** (GitHub CLI) — for GitHub tools; `gh` auth is used as fallback for `GITHUB_TOKEN`
- **gpg** — for encrypted secrets (optional)
- **docker** — for Docker tools (optional)
- **chezmoi** — for dotfile tools (optional)
- **blender** — for 3D model generation (optional)

---

## Installation

```bash
git clone https://github.com/caboose-mcp/server
cd server
export PATH=$PATH:/usr/local/go/bin
go build -o caboose-mcp .
```

Then add to your Claude `.mcp.json`:

```json
{
  "mcpServers": {
    "caboose-mcp": {
      "type": "stdio",
      "command": "/home/caboose/dev/caboose-mcp/caboose-mcp"
    }
  }
}
```

---

## First-time Setup

Run the interactive setup wizard to configure all env vars and write a `.env` file:

```bash
./caboose-mcp --setup
```

The wizard walks through every setting section by section, shows current values (secrets masked), and writes a `0600`-mode `.env` file when done.

To load the env file:
```bash
export $(grep -v '^#' .env | xargs)
```

Or point your service manager / shell profile at it directly.

After setup, verify from Claude:
```
setup_check
```

---

## Usage Modes

### Stdio (default — used by Claude)

```bash
./caboose-mcp
```

Claude communicates with the server over stdin/stdout using the MCP protocol. This is the standard mode when configured in `.mcp.json`.

### HTTP / Streamable HTTP transport

```bash
./caboose-mcp --serve :8080
```

Runs on `/mcp`. Optionally set `MCP_AUTH_TOKEN` to require bearer auth:

```bash
MCP_AUTH_TOKEN=secret ./caboose-mcp --serve :8080
```

### TUI (split-pane terminal dashboard)

```bash
./caboose-mcp --tui
```

Browse sources, pending suggestions, and learning sessions. Key bindings:

| Key | Action |
|-----|--------|
| `tab` / `shift+tab` | Cycle panels |
| `↑` / `↓` | Navigate list |
| `enter` | Show detail |
| `c` | Check selected source for updates |
| `A` | Approve selected suggestion |
| `d` | Delete selected source |
| `r` | Refresh all panels |
| `?` | Toggle help |
| `q` | Quit |

### Setup wizard

```bash
./caboose-mcp --setup
```

See [First-time Setup](#first-time-setup).

---

## Configuration Reference

All configuration is via environment variables. Copy `.env.example` to `.env` and fill in values, or run `--setup`.

| Variable | Required | Description |
|----------|----------|-------------|
| `CLAUDE_DIR` | No | Base data directory. Default: `~/.claude` |
| `GPG_KEY_ID` | For secrets | GPG key for `secret_*` tools. Find with `gpg --list-keys` |
| `SLACK_TOKEN` | For Slack | Bot OAuth token (`xoxb-...`) from api.slack.com/apps |
| `DISCORD_TOKEN` | For Discord | Bot token from discord.com/developers/applications |
| `N8N_WEBHOOK_URL` | For n8n push | Webhook node URL (e.g. `http://localhost:5678/webhook/caboose-events`) |
| `N8N_API_KEY` | No | Sent as `X-N8N-API-KEY` header on webhook calls |
| `GITHUB_TOKEN` | No | Auto-resolved from `gh auth token` if unset |
| `POSTGRES_URL` | For Postgres | `postgres://user:pass@host:5432/dbname` |
| `MONGO_URL` | For MongoDB | `mongodb://host:27017` |
| `BAMBU_IP` | For printing | Local IP of Bambu A1 printer |
| `BAMBU_ACCESS_CODE` | For printing | 8-char code on printer touchscreen |
| `BAMBU_SERIAL` | For printing | Printer serial number |
| `BAMBU_BED_TEMP` | No | Default bed temp °C (default: 55) |
| `BAMBU_NOZZLE_TEMP` | No | Default nozzle temp °C (default: 220) |
| `GREPTILE_API_KEY` | For Greptile | From app.greptile.com |
| `GREPTILE_REPO` | No | Default repo to query. Default: `github/caboose-mcp/server` |
| `CLOUDSYNC_S3_BUCKET` | For S3 sync | S3 bucket for config sync (Gist backend uses `GITHUB_TOKEN`) |
| `MCP_AUTH_TOKEN` | No | Optional bearer token for `--serve` HTTP transport (open/unauthenticated if unset) |

---

## Tool Reference

### Audit & Gate (`audit.go`)

Append-only audit log of every tool call, plus an optional approval gate for dangerous tools.

| Tool | Description |
|------|-------------|
| `audit_list` | Show recent tool execution history (filterable by tool/status) |
| `audit_config` | Enable/disable gate mode; add/remove tools from gate list |
| `audit_pending` | List executions awaiting approval |
| `approve_execution` | Approve a gated execution — runs it immediately |
| `deny_execution` | Deny a gated execution |

Gate flow: gated tools (default: `execute_command`, `si_apply`, `chezmoi_apply`) pause and write a pending file instead of running. The user approves or denies via `approve_execution` / `deny_execution`. When `N8N_WEBHOOK_URL` is set, a `gate_fired` event is pushed to n8n on every gate.

---

### System Commands (`system.go`)

| Tool | Description |
|------|-------------|
| `execute_command` | Run a shell command. Gated by default when audit gate is enabled |

---

### Claude File I/O (`claude.go`)

Safe read/write operations scoped to `CLAUDE_DIR`. Prevents path traversal.

| Tool | Description |
|------|-------------|
| `claude_read_file` | Read a file under `CLAUDE_DIR` |
| `claude_write_file` | Write a file under `CLAUDE_DIR` |
| `claude_append_memory` | Append a line to `CLAUDE_DIR/MEMORY.md` |
| `claude_list_files` | List files in a `CLAUDE_DIR` subdirectory |

---

### Secrets (`secrets.go`)

GPG-encrypted key/value store under `CLAUDE_DIR/secrets/`. Requires `GPG_KEY_ID`.

| Tool | Description |
|------|-------------|
| `secret_set` | Encrypt and store a secret |
| `secret_get` | Decrypt and retrieve a secret |
| `secret_list` | List secret names (values not shown) |

---

### GitHub (`github.go`)

Thin wrappers around the `gh` CLI. `GITHUB_TOKEN` is auto-resolved from `gh auth token`.

| Tool | Description |
|------|-------------|
| `github_search_code` | Search code across GitHub repositories |
| `github_list_repos` | List repositories for a user or organization |
| `github_create_pr` | Create a pull request |

---

### Docker (`docker.go`)

| Tool | Description |
|------|-------------|
| `docker_list_containers` | List all containers (running and stopped) |
| `docker_inspect` | Inspect a container |
| `docker_logs` | Tail container logs |

---

### Databases (`database.go`)

Connection strings from env vars, overridable per-call.

| Tool | Description |
|------|-------------|
| `postgres_query` | Execute a PostgreSQL query |
| `postgres_list_tables` | List tables in a PostgreSQL database |
| `mongodb_query` | Query a MongoDB collection |
| `mongodb_list_collections` | List collections in a MongoDB database |

---

### Slack (`slack.go`)

Requires `SLACK_TOKEN` (Bot OAuth `xoxb-...`).

| Tool | Description |
|------|-------------|
| `slack_post_message` | Post a message to a channel |
| `slack_read_messages` | Read recent messages from a channel |
| `slack_list_channels` | List available channels |

---

### Discord (`discord.go`)

Requires `DISCORD_TOKEN`.

| Tool | Description |
|------|-------------|
| `discord_post_message` | Post a message to a channel |
| `discord_read_messages` | Read recent messages from a channel |
| `discord_list_channels` | List channels in a guild |

---

### Source Monitoring (`sources.go`)

Watch GitHub repos/users, RSS feeds, URLs, npm and PyPI packages for changes.

| Tool | Description |
|------|-------------|
| `source_add` | Add a source to watch |
| `source_list` | List watched sources (filterable by type/tag) |
| `source_edit` | Update a source's metadata |
| `source_remove` | Remove a source |
| `source_check` | Check one or all sources for updates since last check |
| `source_digest` | Check all sources and optionally post digest to Slack/Discord |

Supported types: `github_repo`, `github_user`, `rss`, `url`, `npm`, `pypi`.

When `N8N_WEBHOOK_URL` is set, a `source_changed` event is pushed for each source with updates.

---

### Self-Improvement (`selfimprove.go`)

Scan codebases, generate improvement suggestions with human approval flow, and record errors.

| Tool | Description |
|------|-------------|
| `si_scan_dir` | Scan a directory for stack, issues, TODO markers, tracked .env files |
| `si_git_diff` | Show staged + unstaged diff (or vs a branch) |
| `si_suggest` | Create a pending improvement suggestion |
| `si_list_pending` | List suggestions by status |
| `si_approve` | Approve a suggestion (optionally apply immediately) |
| `si_reject` | Reject and discard a suggestion |
| `si_apply` | Apply an approved suggestion via its `apply_cmd` |
| `si_report_error` | Record an error to `CLAUDE_DIR/errors/` for later triage |
| `si_tech_digest` | Generate a tech digest and optionally post to Slack/Discord |

Auto-apply categories are controlled by `CLAUDE_DIR/selfimprove-allowlist.json`. Suggestions emit `suggestion_created` and errors emit `error_reported` events to n8n.

---

### Learning (`learning.go`)

Spaced-repetition language learning sessions — both programming and spoken languages.

| Tool | Description |
|------|-------------|
| `learn_start` | Start or resume a learning session |
| `learn_exercise` | Get the next exercise in a session |
| `learn_submit` | Submit an answer and get feedback |
| `learn_status` | Show progress across all languages |
| `learn_schedule` | View upcoming review sessions |

Sessions are stored in `CLAUDE_DIR/learning/<language>/`.

---

### Focus Mode (`focus.go`)

ADHD-friendly single-goal focus sessions with a parking lot for distractions.

| Tool | Description |
|------|-------------|
| `focus_start` | Start a focus session with a declared goal and optional timer |
| `focus_status` | Show current goal, elapsed time, remaining time, parked items |
| `focus_park` | Park a distraction without acting on it (saved to parking lot) |
| `focus_end` | End session and print summary with parked items |
| `focus_config` | View/set defaults (duration, goal prefix in replies) |

Background tools (digests, learning nudges) check `IsFocused()` and suppress noise when a session is active. Focus events (`focus_started`, `focus_ended`) are pushed to n8n.

---

### Chezmoi (`chezmoi.go`)

Thin wrappers around the `chezmoi` dotfile manager CLI.

| Tool | Description |
|------|-------------|
| `chezmoi_status` | Show managed file status |
| `chezmoi_diff` | Show pending changes |
| `chezmoi_apply` | Apply pending changes (gated by default) |
| `chezmoi_add` | Add a file to chezmoi management |
| `chezmoi_forget` | Remove a file from chezmoi management |
| `chezmoi_managed` | List all managed files |
| `chezmoi_data` | Show chezmoi template data |
| `chezmoi_init` | Initialize chezmoi with a dotfiles repo |
| `chezmoi_update` | Pull and apply from the dotfiles repo |
| `chezmoi_git` | Run an arbitrary git command in the chezmoi source dir |

---

### Cloud Sync (`cloudsync.go`)

Encrypt and sync `CLAUDE_DIR` config to GitHub Gist (using `GITHUB_TOKEN`) or S3 (using `CLOUDSYNC_S3_BUCKET`). Uses AES-256-GCM encryption.

| Tool | Description |
|------|-------------|
| `cloudsync_push` | Encrypt and upload config |
| `cloudsync_pull` | Download and decrypt config |
| `cloudsync_status` | Show last sync time and backend |
| `cloudsync_setup` | Configure backend (gist or s3) |
| `cloudsync_env_list` | List available remote env snapshots |
| `cloudsync_env_set` | Upload current env as a named snapshot |

---

### Sandbox (`sandbox.go`)

Preview file changes in a temporary copy of a directory before applying them.

| Tool | Description |
|------|-------------|
| `sandbox_run` | Run a command against a temp copy of a directory |
| `sandbox_suggestion` | Generate a suggested change for review |
| `sandbox_list` | List active sandboxes |
| `sandbox_diff` | Show diff between sandbox and original |
| `sandbox_clean` | Remove a sandbox |

---

### Health (`health.go`)

| Tool | Description |
|------|-------------|
| `health_report` | System health: CPU, memory, disk, uptime, Docker container count, systemd failed units |

---

### Setup (`setup.go`)

| Tool | Description |
|------|-------------|
| `setup_check` | Verify all configuration and report OK / MISSING / WARN per feature |
| `setup_init_dirs` | Create required `CLAUDE_DIR` subdirectories |
| `setup_write_env` | Write a `.env` file from provided key=value pairs |
| `setup_n8n_workflows` | Return three importable n8n workflow JSON objects |
| `setup_github_mcp_info` | Explain when to use caboose-mcp vs github/github-mcp-server |

---

### 3D Printing (`printing.go`)

| Tool | Description |
|------|-------------|
| `blender_generate` | Run a headless Blender script to generate a 3D model |
| `bambu_print` | Send a `.3mf` or `.gcode` file to the Bambu A1 via MQTT/TLS |
| `bambu_status` | Get current printer status |
| `bambu_stop` | Stop the current print job |

---

### Mermaid (`mermaid.go`)

| Tool | Description |
|------|-------------|
| `mermaid_generate` | Return a fenced ` ```mermaid ``` ` block for any diagram type |

---

### Greptile (`greptile.go`)

AI-powered semantic code search. Requires `GREPTILE_API_KEY`.

| Tool | Description |
|------|-------------|
| `greptile_query` | Ask a natural-language question about a codebase |
| `greptile_index` | Trigger indexing of a repository |

---

### Notes (`notes.go`)

| Tool | Description |
|------|-------------|
| `note_add` | Append a timestamped note to `CLAUDE_DIR/notes.md` |
| `note_list` | Show recent notes |
| `notes_drive_backup` | Upload notes to Google Drive |
| `notes_drive_restore` | Restore notes from Google Drive |

---

### Calendar (`calendar.go`)

Google Calendar integration with OAuth2 flow.

| Tool | Description |
|------|-------------|
| `calendar_today` | Show today's events |
| `calendar_list` | List events for a date range |
| `calendar_create` | Create an event |
| `calendar_delete` | Delete an event |
| `calendar_auth_url` | Get the OAuth2 authorization URL |
| `calendar_auth_complete` | Complete OAuth2 flow with the callback code |

Credentials: place `credentials.json` (downloaded from GCP console) at `CLAUDE_DIR/google/credentials.json`. Token is auto-saved to `CLAUDE_DIR/google/calendar-token.json` after first auth.

---

### Toolsmith (`toolsmith.go`)

Tools for building and extending the MCP server itself.

| Tool | Description |
|------|-------------|
| `tool_scaffold` | Generate a skeleton for a new tool file |
| `tool_write` | Write a new tool file and register it |
| `tool_rebuild` | Rebuild the binary after changes |
| `tool_list` | List all registered tools |

---

### Persona (`persona.go`)

| Tool | Description |
|------|-------------|
| `agent_persona` | View or update the agent's tone, style, and verbosity settings |

---

### Jokes (`jokes.go`)

| Tool | Description |
|------|-------------|
| `joke` | Get a random programming joke |
| `dad_joke` | Get a random dad joke |

---

## n8n Integration

caboose-mcp can push real-time events to n8n when key things happen. Set `N8N_WEBHOOK_URL` to your n8n Webhook node URL.

### Events pushed

| Event | Triggered by |
|-------|-------------|
| `gate_fired` | A gated tool is blocked pending approval |
| `source_changed` | `source_check` or `source_digest` detects an update |
| `suggestion_created` | `si_suggest` saves a new suggestion |
| `error_reported` | `si_report_error` records an error |
| `focus_started` | `focus_start` begins a session |
| `focus_ended` | `focus_end` closes a session |

All events share the same payload shape:

```json
{
  "type": "gate_fired",
  "id": "1234567890",
  "ts": "2026-03-18T08:00:00Z",
  "source": "caboose-mcp",
  "data": { ... }
}
```

### Getting workflow JSON

```
setup_n8n_workflows
```

Returns three importable n8n v1.x workflow JSON objects:

- **Caboose Event Receiver** — Webhook trigger → Switch on `type` → Set nodes that format messages per event type. Connect the Set nodes to Slack/Discord/email.
- **Caboose Daily Digest** — Schedule (8am) → `source_digest` + `si_tech_digest` via Execute Command.
- **Caboose Nightly Scan** — Schedule (midnight) → `si_scan_dir` + `source_check` via Execute Command.

Import via: n8n → Settings → Import Workflow. Set `CABOOSE_BIN` as an n8n environment variable pointing to the binary.

### Pull-based usage (n8n → caboose-mcp)

n8n Execute Command node pipes MCP JSON to the binary:

```bash
CABOOSE_BIN="${CABOOSE_BIN:-/home/caboose/dev/caboose-mcp/caboose-mcp}"
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"source_digest","arguments":{}}}' | "$CABOOSE_BIN"
```

---

## Architecture

```
main.go
├── --setup   → tui/wizard.go      (interactive config wizard)
├── --tui     → tui/tui.go         (split-pane Bubbletea dashboard)
├── --serve   → HTTP transport      (Streamable HTTP on /mcp)
└── (default) → stdio transport     (used by Claude)

config/
└── config.go   Single Config struct; all env vars loaded once at startup

tools/
├── events.go         EmitEvent() — goroutine webhook push to n8n
├── audit.go          GateOrRun() helper used by gated tools
├── system.go         execute_command (uses GateOrRun)
├── claude.go         File I/O under CLAUDE_DIR
├── secrets.go        GPG encrypt/decrypt
├── github.go         gh CLI wrappers
├── docker.go         docker CLI wrappers
├── database.go       pgx v5 + mongo-driver v2
├── slack.go          Slack Bot API
├── discord.go        Discord REST API
├── sources.go        Source monitoring (6 types)
├── selfimprove.go    si_* tools
├── learning.go       learn_* tools
├── focus.go          Focus mode + parking lot
├── chezmoi.go        chezmoi CLI wrappers
├── cloudsync.go      AES-256-GCM sync to Gist or S3
├── sandbox.go        Temp-dir change preview
├── health.go         System health report
├── setup.go          Setup check + n8n workflow generation
├── printing.go       Blender + Bambu A1 MQTT/TLS
├── mermaid.go        Mermaid diagram blocks
├── greptile.go       Greptile v2 API
├── notes.go          Notes + Google Drive backup
├── calendar.go       Google Calendar OAuth2
├── toolsmith.go      Tool scaffolding
├── persona.go        Agent persona config
└── jokes.go          Joke dispensary

tui/
├── tui.go        Split-pane Bubbletea dashboard (--tui)
├── wizard.go     Interactive setup wizard (--setup)
└── json.go       JSON helpers for TUI
```

### Storage layout (`CLAUDE_DIR`, default `~/.claude`)

```
~/.claude/
├── audit/
│   ├── audit.log              JSONL audit log
│   ├── gate-config.json       Gate mode settings
│   └── pending/               Pending gate approvals
├── errors/                    Errors recorded by si_report_error
├── focus/
│   ├── session.json           Active focus session
│   ├── parked.md              Parking lot (append-only)
│   └── config.json            Focus defaults
├── google/
│   ├── credentials.json       GCP OAuth2 client credentials
│   └── calendar-token.json    OAuth2 token (auto-saved)
├── learning/<lang>/           Learning sessions per language
├── notes.md                   Quick notes (append-only)
├── pending/                   Pending improvement suggestions
├── persona.json               Agent persona settings
├── secrets/                   GPG-encrypted secrets
├── selfimprove-allowlist.json Auto-apply categories
└── sources/                   Watched sources + .seen/ markers
```

---

## License

MIT

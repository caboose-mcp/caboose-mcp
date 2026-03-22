# Tool Reference

118 tools across three tiers:
- **Hosted** (68 tools) — Cloud-safe, run on ECS (`--serve-hosted`)
- **Local** (25 tools) — Require the Pi (`--serve-local`) for hardware/Docker access
- **Common** (3 tools) — Available everywhere (jokes)

**Combined** (all 118) — `--serve` / stdio modes.

---

## Hosted Tools

### Calendar
| Tool | Description |
|------|-------------|
| `calendar_today` | Today's date, day of week, week number |
| `calendar_list` | List upcoming Google Calendar events (default 7 days) |
| `calendar_create` | Create a Google Calendar event |
| `calendar_delete` | Delete a Google Calendar event by ID |
| `calendar_auth_url` | Generate Google OAuth2 consent URL |
| `calendar_auth_complete` | Exchange auth code for token |

**Requires:** `~/.claude/google/credentials.json` (Google OAuth2 client)

---

### Focus
| Tool | Description |
|------|-------------|
| `focus_start` | Start a focus session with a declared goal and optional timer |
| `focus_status` | Show current session: goal, time remaining, parked items |
| `focus_park` | Park a distraction without breaking focus |
| `focus_end` | End the session, returns summary + parked items |
| `focus_config` | View or update focus defaults (duration, prefix mode) |

**Stores:** `~/.claude/focus/session.json`, `~/.claude/focus/parked.md`

---

### Notes
| Tool | Description |
|------|-------------|
| `note_add` | Append a timestamped note to `~/.claude/notes.md` |
| `note_list` | List recent notes, optionally filtered by tag |
| `notes_drive_backup` | Upload `notes.md` to Google Drive |
| `notes_drive_restore` | Download `notes.md` from Google Drive |

---

### Learning
| Tool | Description |
|------|-------------|
| `learn_start` | Start or resume a language learning session (code or spoken) |
| `learn_status` | Show progress across all languages |
| `learn_exercise` | Get context for the next exercise in a session |
| `learn_submit` | Record an answer and feedback |
| `learn_schedule` | View or update the daily learning schedule |

**Stores:** `~/.claude/learning/<lang>/<session>.json`

---

### Slack
| Tool | Description |
|------|-------------|
| `slack_list_channels` | List channels the bot has access to |
| `slack_read_messages` | Read recent messages from a channel |
| `slack_post_message` | Post a message to a channel |

**Requires:** `SLACK_TOKEN`

---

### Discord
| Tool | Description |
|------|-------------|
| `discord_list_channels` | List channels in a guild |
| `discord_read_messages` | Read recent messages from a channel |
| `discord_post_message` | Post a message to a channel |
| `discord_webhook_post` | Post via incoming webhook (no bot token needed) |

**Requires:** `DISCORD_TOKEN` (bot), `DISCORD_WEBHOOK_URL` (webhook)

---

### GitHub
| Tool | Description |
|------|-------------|
| `github_list_repos` | List repositories for an owner |
| `github_search_code` | Search code across GitHub |
| `github_create_pr` | Create a pull request |

**Requires:** `GITHUB_TOKEN` or `gh auth token`

---

### Database
| Tool | Description |
|------|-------------|
| `postgres_list_tables` | List tables in a PostgreSQL database |
| `postgres_query` | Execute a SQL query |
| `mongodb_list_collections` | List collections in a MongoDB database |
| `mongodb_query` | Query a MongoDB collection |

**Requires:** `POSTGRES_URL`, `MONGO_URL`

---

### Health
| Tool | Description |
|------|-------------|
| `health_report` | CPU load, memory, disk, uptime, systemd services, Docker |

---

### Secrets
| Tool | Description |
|------|-------------|
| `secret_list` | List names of all stored secrets |
| `secret_get` | Decrypt and return a stored secret |
| `secret_set` | Encrypt and store a secret using GPG |

**Requires:** `GPG_KEY_ID`, GPG key present on host

---

### Cloud Sync
| Tool | Description |
|------|-------------|
| `cloudsync_status` | Show sync config and last push/pull times |
| `cloudsync_env_list` | List env var keys in the sync bundle |
| `cloudsync_env_set` | Add/update an env var in the bundle |
| `cloudsync_push` | Encrypt and upload config bundle to S3 or Gist |
| `cloudsync_pull` | Download and decrypt config bundle |
| `cloudsync_setup` | First-time setup (creates S3 bucket or Gist) |

**Requires:** `CLOUDSYNC_S3_BUCKET` or GitHub Gist via `gh` CLI

---

### Self Improve (si_*)
| Tool | Description |
|------|-------------|
| `si_scan_dir` | Scan a directory for tech stack and quality hints |
| `si_git_diff` | Show git diff for a repo |
| `si_suggest` | Create a pending improvement suggestion |
| `si_list_pending` | List pending suggestions |
| `si_approve` | Approve a suggestion (optionally auto-apply) |
| `si_apply` | Apply an approved suggestion |
| `si_reject` | Reject and discard a suggestion |
| `si_report_error` | Record an error for later triage |
| `si_tech_digest` | Generate a tech digest, optionally post to Slack/Discord |

**Stores:** `~/.claude/pending/`, `~/.claude/errors/`

---

### Setup
| Tool | Description |
|------|-------------|
| `setup_check` | Check configuration and report enabled/disabled features |
| `setup_init_dirs` | Initialize `~/.claude/` subdirectories |
| `setup_write_env` | Write a `.env` file with provided key=value pairs |
| `setup_n8n_workflows` | Return example n8n workflow JSON |
| `setup_github_mcp_info` | Explain GitHub MCP server vs fafb |
| `setup_bot_configure` | Configure Slack/Discord bot settings |

---

### Sources
| Tool | Description |
|------|-------------|
| `source_list` | List watched sources |
| `source_add` | Add a source (github_repo, rss, url, npm, pypi, github_user) |
| `source_edit` | Edit a source by ID |
| `source_remove` | Remove a source |
| `source_check` | Check one or all sources for new activity |
| `source_digest` | Check all sources and post digest to Slack/Discord |

**Stores:** `~/.claude/sources.json`

---

### Audit
| Tool | Description |
|------|-------------|
| `audit_list` | Show recent tool execution log |
| `audit_pending` | List executions waiting for approval |
| `audit_config` | View or modify gate configuration |
| `approve_execution` | Approve a gated tool execution |
| `deny_execution` | Deny a gated tool execution |

**Stores:** `~/.claude/audit/audit.log`, `~/.claude/audit/gate-config.json`

---

### Auth
| Tool | Description |
|------|-------------|
| `auth_create_token` | Create a JWT token (magic link or named API token) |
| `auth_list_tokens` | List all issued tokens |
| `auth_revoke_token` | Revoke a token by ID |
| `auth_link_identity` | Link a platform identity (Discord/Slack user ID) to a token |
| `auth_list_identities` | List all linked platform identities |
| `auth_unlink_identity` | Unlink a platform identity |

**Stores:** `~/.claude/auth/` (JWT secret + token store)

---

### Sandbox
| Tool | Description |
|------|-------------|
| `sandbox_run` | Clone a dir to temp, run a command, return diff |
| `sandbox_list` | List active sandboxes |
| `sandbox_diff` | Re-run diff for an existing sandbox |
| `sandbox_suggestion` | Preview a pending suggestion in a sandbox |
| `sandbox_clean` | Delete sandboxes (by ID or age) |

---

### Persona
| Tool | Description |
|------|-------------|
| `agent_persona` | Get, set, or reset the agent's persona config (name, tone, verbosity, interests) |

**Stores:** `~/.claude/persona.json`

---

### Environment
| Tool | Description |
|------|-------------|
| `env_check` | Check which dev tools are installed |
| `env_fix` | Install missing dev tools |

---

### Claude Files
| Tool | Description |
|------|-------------|
| `claude_list_files` | List files under `~/.claude/` |
| `claude_read_file` | Read a file under `~/.claude/` |
| `claude_write_file` | Write a file under `~/.claude/` |
| `claude_append_memory` | Append content to `CLAUDE.md` or a named memory file |

---

### Mermaid
| Tool | Description |
|------|-------------|
| `mermaid_generate` | Generate a Mermaid diagram (db_schema, docker, flowchart, sequence) |

---

### Greptile
| Tool | Description |
|------|-------------|
| `greptile_query` | Ask a question about a codebase using the Greptile API |
| `greptile_index` | Trigger Greptile to index a repository |

**Requires:** `GREPTILE_API_KEY`

---

### Fun (Common Tools)
| Tool | Description |
|------|-------------|
| `joke` | Tell a programming joke |
| `dad_joke` | Tell a dad joke |

---

## Local Tools

These tools require the binary to run on a machine with local hardware/network access (e.g. the Pi).

### Docker
| Tool | Description |
|------|-------------|
| `docker_list_containers` | List running (or all) containers |
| `docker_logs` | Fetch container logs |
| `docker_inspect` | Return full container JSON config |

**Requires:** Docker daemon accessible on the host

---

### System
| Tool | Description |
|------|-------------|
| `execute_command` | Execute a shell command, return stdout+stderr |

**Note:** Gated by the audit system when gate mode is enabled. Use `audit_config` to control access.

---

### Printing
| Tool | Description |
|------|-------------|
| `bambu_status` | Get current status from Bambu A1 printer over MQTT |
| `bambu_print` | Start a print job (.3mf or .gcode) |
| `bambu_stop` | Stop the active print job |
| `blender_generate` | Run a Blender Python script headlessly to generate a 3D file |

**Requires (Bambu):** `BAMBU_IP`, `BAMBU_ACCESS_CODE`, `BAMBU_SERIAL`
**Requires (Blender):** `blender` binary in `PATH`

---

### Chezmoi
| Tool | Description |
|------|-------------|
| `chezmoi_status` | Show which managed files differ from source state |
| `chezmoi_diff` | Preview changes without applying |
| `chezmoi_apply` | Apply source state to home directory |
| `chezmoi_add` | Add a file/directory to chezmoi management |
| `chezmoi_forget` | Stop managing a file (leaves target intact) |
| `chezmoi_managed` | List all files managed by chezmoi |
| `chezmoi_data` | Show chezmoi template data variables |
| `chezmoi_git` | Run a git command inside the chezmoi source directory |
| `chezmoi_update` | Pull latest from source repo and apply |
| `chezmoi_init` | Initialize chezmoi, optionally from a git repo |

**Requires:** `chezmoi` binary in `PATH`

---

### Toolsmith
| Tool | Description |
|------|-------------|
| `tool_list` | List tool source files and the tools each registers |
| `tool_scaffold` | Generate boilerplate Go source for a new tool |
| `tool_write` | Write Go source to `tools/<file>.go` and patch `main.go` |
| `tool_rebuild` | Run `go build` and return compiler output |

**Requires:** Go toolchain on the host

---

### Agency
| Tool | Description |
|------|-------------|
| `agency_list` | List all loaded agent spec files from `~/.claude/agents/` |
| `agency_detect` | Detect best-matching agent persona for a message using keyword scoring |
| `agency_hint` | Return a formatted tool hint block for a message (advisory, not enforced) |

**Requires:** Agent spec markdown files in `~/.claude/agents/` (from [agency-agents](https://github.com/msitarzewski/agency-agents))

---

## Configuration

All configuration is via environment variables. Copy `.env.example` to `.env`:

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | Yes (bots) | Claude API key for Slack/Discord bots |
| `SLACK_TOKEN` | Slack tools | Bot OAuth token (`xoxb-...`) |
| `SLACK_APP_TOKEN` | Slack bot | App-level token (`xapp-...`) for Socket Mode |
| `SLACK_BOT_CHANNELS` | Optional | Comma-separated channel IDs to respond in |
| `DISCORD_TOKEN` | Discord tools | Bot token |
| `DISCORD_WEBHOOK_URL` | Optional | Incoming webhook for outbound notifications |
| `DISCORD_BOT_CHANNELS` | Optional | Comma-separated channel IDs |
| `GITHUB_TOKEN` | GitHub tools | Personal access token (falls back to `gh auth token`) |
| `GPG_KEY_ID` | Secrets | GPG key ID for encrypting secrets |
| `POSTGRES_URL` | DB tools | PostgreSQL connection string |
| `MONGO_URL` | DB tools | MongoDB connection string |
| `GREPTILE_API_KEY` | Greptile | API key |
| `BAMBU_IP` | Bambu | Printer local IP |
| `BAMBU_ACCESS_CODE` | Bambu | 8-digit code from printer screen |
| `BAMBU_SERIAL` | Bambu | Printer serial number |
| `MCP_AUTH_TOKEN` | HTTP server | Bearer token for `--serve*` modes |
| `CLAUDE_DIR` | Optional | Override `~/.claude/` data directory |
| `N8N_WEBHOOK_URL` | n8n | Webhook URL for n8n integration |

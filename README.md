# fafb

Personal AI toolserver — **130 MCP tools** exposed to Claude, VS Code, and chat bots via a Go server hosted on AWS ECS, with Discord OAuth, admin approval workflows, and automatic tool synchronization.

> Something selfishly for me but hopefully useful for others.
>
> **⚡ NEW:** Ecosystem Manager (Phases 1-6) — Discord OAuth, auto-deployment, tool creation UI, admin approval workflows, and 130+ tools extracted and synchronized.

[![Deploy Infra](https://github.com/fafb/fafb/actions/workflows/deploy-infra.yml/badge.svg)](https://github.com/fafb/fafb/actions/workflows/deploy-infra.yml)
[![Deploy Bots](https://github.com/fafb/fafb/actions/workflows/deploy-bots.yml/badge.svg)](https://github.com/fafb/fafb/actions/workflows/deploy-bots.yml)
[![Deploy App](https://github.com/fafb/fafb/actions/workflows/deploy-app.yml/badge.svg)](https://github.com/fafb/fafb/actions/workflows/deploy-app.yml)
[![Release](https://github.com/fafb/fafb/actions/workflows/release.yml/badge.svg)](https://github.com/fafb/fafb/actions/workflows/release.yml)
[![Go 1.24](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)](https://go.dev)
[![MCP Live](https://img.shields.io/website?url=https%3A%2F%2Fmcp.chrismarasco.io%2Fhealth&label=mcp.chrismarasco.io&up_message=live&logo=amazonaws)](https://mcp.chrismarasco.io/ui/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**Live endpoint:** `https://mcp.chrismarasco.io/mcp` · **Web UI:** `https://mcp.chrismarasco.io/ui/`

> [!WARNING]
> **Experimental software — use at your own risk.** This project is under active development and has not been fully tested. Tools may behave unexpectedly, modify data, or fail without warning. See the [Disclaimer](#disclaimer) for full terms.

---

## Architecture

```mermaid
graph TD
    Claude["Claude Code (stdio)"] -->|all 118 tools| Binary["fafb (Pi)"]
    VSCode["VS Code / Bruno"] -->|HTTPS| ALB["ALB · mcp.chrismarasco.io"]
    ALB --> Serve["ECS · --serve-hosted\n68 hosted tools"]
    Slack["Slack"] --> Bots["ECS · --bots\nSlack + Discord gateway"]
    Discord["Discord"] --> Bots
    Bots -->|Claude Haiku| Agent["Bot agent loop"]
    Binary -->|LAN MQTT| Bambu["Bambu A1 Printer"]
    Binary -->|socket| DockerD["Docker daemon"]
    Serve & Bots --> SM["AWS Secrets Manager\nfafb/env"]
```

---

## Tool Tiers

Tools are split so the cloud server only exposes what's safe remotely.

| Tier | Flag | Count | What's included |
|------|------|-------|-----------------|
| **Hosted** | `--serve-hosted` | 68 | Calendar, Slack, Discord, GitHub, Notes, Focus, Learning, Sources, CloudSync, Audit, Auth, Health, Secrets, DB, Env, Mermaid, Greptile, Sandbox, Persona, Jokes, Setup, Claude Files, Self-Improve, **Repo Management** (create, test, approve, deploy), **Gamma** (presentations) |
| **Local** | `--serve-local` | 25 | Docker, execute_command, Bambu, Blender, Chezmoi, Toolsmith, Agency |
| **Common** | (any mode) | 3 | Jokes, Claude Files, Setup |
| **Combined** | `--serve` / stdio | 130 | Everything (common + hosted + local + new tools) |

Full reference: [docs/tools.md](docs/tools.md) · [ECOSYSTEM.md](ECOSYSTEM.md)

---

## ⚡ Ecosystem Manager (NEW)

**Phases 1-6 Complete:** Secure, integrated toolkit for managing 130+ MCP tools with Discord OAuth, admin approval workflows, and automatic tool synchronization.

### Key Features

✅ **Phase 1:** Discord OAuth2 PKCE flow for user authentication
✅ **Phase 2:** Auto-extract 130 tools + auto-sync UI on push to main
✅ **Phase 3:** Tool creation, testing, and admin approval workflow
✅ **Phase 4:** Auto-generate Gamma presentation decks
✅ **Phase 5:** GitHub PR-based tool submission with CI tests
✅ **Phase 6:** Admin UI forms + parameter validation

### Create a Tool

**Via Web UI:**
```
1. Go to https://ui.mcp.chrismarasco.io/admin/create-tool
2. Fill in tool details (name, description, parameters)
3. Click "Create Tool" → saved to ~/.claude/pending-tools/
4. Test via /admin/pending-tools
5. Admin approves → commits to main → auto-deploys
```

**Via MCP:**
```bash
fafb repo_create_tool --name "my_tool" --description "Does X" --tier "hosted"
fafb repo_test_tool --tool_name "my_tool" --test_input_json '{}'
fafb repo_approve_tool --tool_name "my_tool"
```

See [ECOSYSTEM.md](ECOSYSTEM.md) for complete workflow and API reference.

---

## Quick Start

### Connect to the live server

**Claude Code CLI:**
```bash
# HTTP — works in any project
claude mcp add --transport http MCP_CABOOSE https://mcp.chrismarasco.io/mcp

# User scope — available across all projects
claude mcp add --transport http --scope user MCP_CABOOSE https://mcp.chrismarasco.io/mcp
```

**VS Code** — add to `.vscode/mcp.json`:
```json
{
  "servers": {
    "fafb": {
      "type": "http",
      "url": "https://mcp.chrismarasco.io/mcp"
    }
  }
}
```

**Bruno** — open the `bruno/` folder as a collection.

### Run locally (Claude Code on Pi)

```bash
cd packages/server && go build -o fafb .

# Interactive setup wizard — writes .env
./fafb --setup

# Terminal UI dashboard
./fafb --tui

./fafb                # stdio — all tools (Claude Code)
./fafb --serve        # HTTP :8080 — all tools
./fafb --serve-hosted # HTTP :8080 — hosted tools only
./fafb --serve-local  # HTTP :8080 — local tools only
./fafb --bots         # Slack + Discord bots (blocks)
```

`.mcp.json` for Claude Code:
```json
{
  "mcpServers": {
    "caboose": { "command": "/path/to/packages/server/fafb", "type": "stdio" }
  }
}
```

Or via CLI:
```bash
claude mcp add MCP_CABOOSE -- /path/to/fafb
```

### Run with Docker Compose (server + n8n)

```bash
cp .env.example .env          # fill in secrets
docker compose -f docker/docker-compose.yml up -d
```

| Service | URL |
|---------|-----|
| MCP server | `http://localhost:8080/mcp` |
| n8n | `http://localhost:5678` |

Full setup instructions for both local and hosted deploys: [docs/setup.md](docs/setup.md)

---

## CI / CD

| Workflow | Trigger | Action |
|----------|---------|--------|
| `ci.yml` | PR / push to main | Lint, test, build (amd64+arm64), e2e, UI build + changelog generation |
| `deploy-infra.yml` | Push to `terraform/aws/**` or manual | `tofu apply` + sync secrets to AWS Secrets Manager |
| `deploy-bots.yml` | Push to `packages/server/**` on main | Build image → ECR → redeploy `fafb-bots` |
| `deploy-app.yml` | Push to `packages/server/**` or `docker/Dockerfile` on main | Build image → ECR → redeploy `fafb-serve` → index Greptile |
| `release.yml` | Push to main or `v*.*.*` tag | Build linux/amd64 + linux/arm64 → GitHub Release (auto date-tag on merge) |

### Releases

Releases are created **automatically on every merge to main** with a date-based tag (`vYYYY.MM.DD.N`). For a versioned release, push a `v*.*.*` tag:

```bash
git tag v1.2.0
git push origin v1.2.0
```

---

## Infrastructure

AWS resources managed by OpenTofu in `terraform/aws/`:

- **ECS Fargate** — `fafb-bots` (`--bots`) + `fafb-serve` (`--serve-hosted`)
- **ALB** — HTTPS 443, HTTP→HTTPS redirect, `mcp.chrismarasco.io`
- **ACM** — TLS cert, DNS-validated via Route53
- **ECR** — Docker image registry (lifecycle: keep last 5)
- **Secrets Manager** — `fafb/env` — secrets injected into ECS tasks at startup
- **S3** — encrypted config sync bucket
- **CloudWatch Logs** — 30-day retention per service

```bash
cd terraform/aws
cp terraform.tfvars.example terraform.tfvars
tofu init && tofu plan && tofu apply
```

---

## Adding a Tool

```bash
tool_scaffold   # generate tools/mytool.go skeleton
tool_write      # write the file
tool_rebuild    # go build
```

Or edit `packages/server/tools/` directly — one `.go` file per feature group.

---

## Repository Layout

```
packages/server/         Go MCP server (117 tools)
  tools/                 One .go file per feature group
  config/config.go       All env vars → Config struct
  main.go                Flags, server builders, HTTP mux, bot runner
packages/vscode-extension/  VS Code extension
bruno/                   Bruno collection (120 requests, 24 categories)
docker/Dockerfile        Multi-stage: Go build → alpine runtime
terraform/aws/           OpenTofu — ECS, ALB, ACM, ECR, S3, Secrets Manager
.github/workflows/       ci, deploy-infra, deploy-bots, deploy-app, release
docs/
  tools.md               Full tool reference
  setup.md               Local + hosted deployment guide, JWT RBAC, AWS costs
```

---

## Disclaimer

**fafb is experimental software provided "as is", without warranty of any kind, express or implied.**

By using this software you acknowledge and agree that:

- This project is under active development and **has not been fully tested** in all environments or configurations.
- Tools may execute shell commands, modify files, query external APIs, send messages, or interact with cloud infrastructure. **Outcomes are not guaranteed to be correct or safe.**
- No warranty is provided — express, implied, statutory, or otherwise — including but not limited to warranties of merchantability, fitness for a particular purpose, or non-infringement.
- The author(s) shall not be liable for any direct, indirect, incidental, special, consequential, or exemplary damages arising from the use or inability to use this software, even if advised of the possibility of such damages.
- **Do not use in production systems, with sensitive or regulated data, or in any context where failures could cause harm**, until you have reviewed and validated the relevant tools for your use case.

This disclaimer is in addition to and does not limit the terms of the [MIT License](LICENSE).

The `FAFB_ENV=stable` flag may be set to suppress experimental warnings once a deployment has been reviewed and accepted for a given use case — this does not alter or waive any of the above terms.

---

## License

[MIT](LICENSE) — Copyright (c) 2025 [Chris Marasco](https://chris.marasco.io)

---

## Credits

Built with love, chaos, and coffee by:

| Contributor | Role |
|-------------|------|
| [Chris Marasco](https://chris.marasco.io) ([@cxm6467](https://github.com/cxm6467)) | Architect, product owner, primary developer |
| [Claude](https://claude.ai) (Anthropic) | AI pair programmer and code generation |
| [Claude Code](https://claude.ai/claude-code) | Agentic CLI — implementation, refactoring, debugging |
| [GitHub Copilot](https://github.com/features/copilot) | Inline completion and suggestions |

---

Thanks bear for the name idea!

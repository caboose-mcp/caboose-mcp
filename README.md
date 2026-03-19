# caboose-mcp

Personal AI toolserver — 108 MCP tools exposed to Claude, VS Code, and chat bots via a Go server hosted on AWS ECS.

[![Deploy Infra](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/deploy-infra.yml/badge.svg)](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/deploy-infra.yml)
[![Deploy Bots](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/deploy-bots.yml/badge.svg)](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/deploy-bots.yml)
[![Deploy App](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/deploy-app.yml/badge.svg)](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/deploy-app.yml)
[![Release](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/release.yml/badge.svg)](https://github.com/caboose-mcp/caboose-mcp/actions/workflows/release.yml)
[![Go 1.24](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)](https://go.dev)
[![MCP Live](https://img.shields.io/website?url=https%3A%2F%2Fmcp.chrismarasco.io%2Fmcp&label=mcp.chrismarasco.io&logo=amazonaws)](https://mcp.chrismarasco.io/mcp)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**Live endpoint:** `https://mcp.chrismarasco.io/mcp` (bearer auth required)

---

## Architecture

```mermaid
graph TD
    Claude["Claude Code (stdio)"] -->|all 108 tools| Binary["caboose-mcp (Pi)"]
    VSCode["VS Code / Bruno"] -->|HTTPS + Bearer| ALB["ALB · mcp.chrismarasco.io"]
    ALB --> Serve["ECS · --serve-hosted\n88 hosted tools"]
    Slack["Slack"] --> Bots["ECS · --bots\nSlack + Discord gateway"]
    Discord["Discord"] --> Bots
    Bots -->|Claude Haiku| Agent["Bot agent loop"]
    Binary -->|LAN MQTT| Bambu["Bambu A1 Printer"]
    Binary -->|socket| DockerD["Docker daemon"]
    Serve & Bots --> SM["AWS Secrets Manager\ncaboose-mcp/env"]
```

---

## Tool Tiers

Tools are split so the cloud server only exposes what's safe remotely.

| Tier | Flag | Count | What's included |
|------|------|-------|-----------------|
| **Hosted** | `--serve-hosted` | 88 | Calendar, Slack, Discord, GitHub, Notes, Focus, Learning, Sources, CloudSync, Audit, Health, Secrets, DB, Mermaid, Greptile, Sandbox, Persona, Jokes, Setup |
| **Local** | `--serve-local` | 20 | Docker, execute_command, Bambu, Blender, Chezmoi, Toolsmith |
| **Combined** | `--serve` / stdio | 108 | Everything |

Full reference: [docs/tools.md](docs/tools.md)

---

## Quick Start

### Connect to the live server

**VS Code** — add to `.vscode/mcp.json`:
```json
{
  "servers": {
    "caboose-mcp": {
      "type": "http",
      "url": "https://mcp.chrismarasco.io/mcp",
      "headers": { "Authorization": "Bearer <MCP_AUTH_TOKEN>" }
    }
  }
}
```

**Bruno** — open the `bruno/` folder as a collection, set the `production` environment `authToken`.

Get the token:
```bash
aws secretsmanager get-secret-value --secret-id caboose-mcp/env \
  --query 'SecretString' --output text \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['MCP_AUTH_TOKEN'])"
```

### Run locally (Claude Code on Pi)

```bash
cd packages/server && go build -o caboose-mcp .

# Interactive setup wizard — writes .env
./caboose-mcp --setup

# Terminal UI dashboard
./caboose-mcp --tui

./caboose-mcp                # stdio — all tools (Claude Code)
./caboose-mcp --serve        # HTTP :8080 — all tools
./caboose-mcp --serve-hosted # HTTP :8080 — hosted tools only
./caboose-mcp --serve-local  # HTTP :8080 — local tools only
./caboose-mcp --bots         # Slack + Discord bots (blocks)
```

`.mcp.json` for Claude Code:
```json
{
  "mcpServers": {
    "caboose": { "command": "/path/to/packages/server/caboose-mcp", "type": "stdio" }
  }
}
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
| `deploy-infra.yml` | Push to `terraform/aws/**` or manual | `tofu apply` + sync secrets to AWS Secrets Manager |
| `deploy-bots.yml` | Push to `packages/server/**` on main | Build AMD64 image → ECR → redeploy `caboose-mcp-bots` |
| `deploy-app.yml` | Push to `packages/server/**` on main | Build AMD64 image → ECR → redeploy `caboose-mcp-serve` |
| `release.yml` | Push tag `v*.*.*` | Build linux/amd64 + linux/arm64 binaries → GitHub Release |

### Creating a release

```bash
git tag v1.0.0
git push origin v1.0.0
```

This triggers `release.yml` which builds binaries for both architectures and attaches them to a GitHub Release.

---

## Infrastructure

AWS resources managed by OpenTofu in `terraform/aws/`:

- **ECS Fargate** — `caboose-mcp-bots` (`--bots`) + `caboose-mcp-serve` (`--serve-hosted`)
- **ALB** — HTTPS 443, HTTP→HTTPS redirect, `mcp.chrismarasco.io`
- **ACM** — TLS cert, DNS-validated via Route53
- **ECR** — Docker image registry (lifecycle: keep last 5)
- **Secrets Manager** — `caboose-mcp/env` — secrets injected into ECS tasks at startup
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
packages/server/         Go MCP server
  tools/                 One .go file per feature group (108 tools)
  config/config.go       All env vars → Config struct
  main.go                Flags, server builders, bot runner
bruno/                   Bruno collection (120 requests, 24 categories)
terraform/aws/           OpenTofu — ECS, ALB, ACM, ECR, S3, Secrets Manager
.github/workflows/       deploy-infra, deploy-bots, deploy-app, release
docs/
  tools.md               Full tool reference
```

---

## License

MIT

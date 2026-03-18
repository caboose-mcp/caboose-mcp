# caboose-mcp

Personal AI toolserver monorepo. Exposes 105 MCP tools to Claude and VS Code via a Go server, with a companion VS Code extension and n8n workflow automation.

## Packages

| Package | Description |
|---------|-------------|
| [`packages/server`](packages/server) | Go MCP server — 105 tools across 25 groups (focus, slack, docker, calendar, etc.) |
| [`packages/vscode-extension`](packages/vscode-extension) | VS Code sidebar + status bar; connects to the server over stdio or HTTP |
| [`packages/n8n`](packages/n8n) | Custom n8n image with pre-loaded caboose-mcp workflows |

## Quick Start (Docker)

```bash
# 1. Clone
git clone https://github.com/caboose-mcp/caboose-mcp
cd caboose-mcp

# 2. Configure
cp .env.example .env
# Edit .env — at minimum set SLACK_TOKEN, GITHUB_TOKEN, GPG_KEY_ID

# 3. Start
docker compose -f docker/docker-compose.yml up -d

# MCP server:  http://localhost:8080/mcp
# n8n:         http://localhost:5678
```

The server mounts your `~/.claude` directory so all data (notes, sessions, secrets, audit logs) persists on the host. n8n workflows are auto-imported on first run.

## Quick Start (Local / Claude stdio)

```bash
# Build the server
cd packages/server
export PATH=$PATH:/usr/local/go/bin
go build -o caboose-mcp .

# Add to Claude's .mcp.json
{
  "mcpServers": {
    "caboose": {
      "type": "stdio",
      "command": "/path/to/packages/server/caboose-mcp"
    }
  }
}
```

## VS Code Extension

```bash
cd packages/vscode-extension
pnpm install
pnpm compile
# Press F5 in VS Code to launch the extension host
```

Configure via Settings → Caboose MCP:
- `cabooseMcp.transport`: `http` (default) or `stdio`
- `cabooseMcp.host` / `cabooseMcp.port`: point at the Docker server
- `cabooseMcp.binaryPath`: path to the built binary (stdio mode)

## n8n Integration

Three workflows are pre-loaded:
- **Event Receiver** — receives `gate_fired`, `source_changed`, `focus_started`, etc.
- **Daily Digest** — 8am: calls `source_digest` + `si_tech_digest`
- **Nightly Scan** — midnight: calls `si_scan_dir` + `source_check`

See [docs/n8n.md](docs/n8n.md) for full documentation.

## Infrastructure (Terraform)

Provisions AWS resources: IAM user (Bedrock), S3 bucket (cloudsync), ECR repo (Docker images).

```bash
cd terraform/aws
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars

terraform init
terraform plan
terraform apply
```

See [terraform/aws/README.md](terraform/aws/README.md) for details.

## Development

### Root scripts (pnpm)

```bash
pnpm build              # compile extension + build Go server
pnpm server:build       # Go build only
pnpm extension:compile  # tsc compile only
pnpm extension:package  # package .vsix
pnpm docker:build       # docker build
pnpm docker:up          # docker compose up -d
pnpm docker:down        # docker compose down
pnpm docker:logs        # follow compose logs
```

### Go server

```bash
cd packages/server
go build -o caboose-mcp .
go vet ./...
```

No database or migration step — the server uses flat JSON files under `~/.claude/`.

### Adding a tool

```bash
# From Claude or the extension, call:
tool_scaffold   # generates a new tools/mytool.go skeleton
tool_write      # writes the file
tool_rebuild    # runs go build
```

Or edit `packages/server/tools/` directly and run `go build`.

## Releases

| Tag pattern | Action |
|-------------|--------|
| `server/v1.2.3` | Builds Go binary for linux/amd64 + arm64; pushes Docker image to GHCR |
| `extension/v0.2.0` | Packages `.vsix`; creates GitHub Release with the asset attached |

## Repository layout

```
packages/
  server/              Go MCP server
  vscode-extension/    VS Code extension (TypeScript)
  n8n/                 Custom n8n image + pre-built workflow JSON
docker/
  Dockerfile           Multi-stage Go build → alpine runtime
  docker-compose.yml   server + n8n services
docs/
  n8n.md               n8n integration guide
terraform/
  aws/                 IAM, S3, ECR
.github/workflows/
  ci.yml               Go vet/build + TS compile on PR
  docker.yml           Build + push to GHCR on main / server/v* tag
  extension.yml        Package + release VSIX on extension/v* tag
```

## License

MIT

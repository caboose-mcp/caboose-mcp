# packages/n8n

Custom n8n image that pre-loads the three caboose-mcp workflows on first run.

## Workflows

| File | Name | Trigger |
|------|------|---------|
| `workflows/event-receiver.json` | Caboose Event Receiver | Webhook — receives events pushed by the MCP server |
| `workflows/daily-digest.json` | Caboose Daily Digest | Cron 8am — calls `source_digest` + `si_tech_digest` |
| `workflows/nightly-scan.json` | Caboose Nightly Scan | Cron midnight — calls `si_scan_dir` + `source_check` |

## How it works

The `docker-entrypoint.sh` runs `n8n import:workflow` for each JSON file before
starting n8n. A sentinel file at `/home/node/.n8n/.caboose-imported` prevents
re-importing on subsequent restarts.

## First run

```bash
docker compose up -d
# → n8n starts at http://localhost:5678
# → complete owner account setup in the UI
# → go to Workflows — three caboose-mcp workflows are already imported
# → activate each workflow to enable it
```

## Customising workflows

Edit the JSON files in `workflows/` and rebuild:

```bash
docker compose build n8n
docker compose up -d n8n
```

To reset and re-import (delete the sentinel):

```bash
docker compose exec n8n rm /home/node/.n8n/.caboose-imported
docker compose restart n8n
```

## n8n ↔ server communication

All workflows call the MCP server via HTTP Request nodes using the
`CABOOSE_MCP_URL` environment variable (`http://server:8080/mcp` inside Docker).

Set `CABOOSE_MCP_TOKEN` (same as `MCP_AUTH_TOKEN`) in `.env` if bearer auth is enabled.

See [../../docs/n8n.md](../../docs/n8n.md) for full integration documentation.

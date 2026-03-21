# n8n Integration

fafb and n8n work together in two directions:

```
fafb  ──push events──►  n8n webhooks
n8n          ──HTTP Request──►  fafb /mcp
```

## Setup

1. Start the stack: `docker compose up -d`
2. Open n8n at http://localhost:5678 and complete first-run setup.
3. In the VS Code extension (or via Claude), call `setup_n8n_workflows` — it returns
   three importable workflow JSON objects. Import each via **Settings → Import Workflow**.

## Workflows

### Caboose Event Receiver (server → n8n)

- **Trigger**: Webhook node at `/webhook/caboose-events`
- **Switch** on `body.type`:
  - `gate_fired` → notify you that a gated command is waiting for approval
  - `source_changed` → post a Slack/Discord message with the source diff
  - `suggestion_created` → log the suggestion for review
  - `error_reported` → alert on new errors
  - `focus_started` / `focus_ended` → toggle a focus Slack status

The server uses the container-internal URL `http://n8n:5678/webhook/caboose-events`
(already set in docker-compose). No additional config needed.

### Caboose Daily Digest (n8n → server, scheduled)

- **Trigger**: Schedule at 8:00 AM
- **HTTP Request** nodes POST to `http://server:8080/mcp`:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "source_digest",
    "arguments": { "post_to": "slack" }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "si_tech_digest",
    "arguments": { "post_to": "slack" }
  }
}
```

### Caboose Nightly Scan (n8n → server, scheduled)

- **Trigger**: Schedule at midnight
- Calls `si_scan_dir` and `source_check` in sequence.

## Calling tools from n8n (HTTP Request node)

All tool calls are JSON-RPC 2.0 POST requests to `http://server:8080/mcp`.

**Node settings**:
- Method: `POST`
- URL: `{{ $env.CABOOSE_MCP_URL }}`  (set to `http://server:8080/mcp` in compose)
- Headers: `Content-Type: application/json`
  - Optional: `Authorization: Bearer {{ $env.CABOOSE_MCP_TOKEN }}` (only if `MCP_AUTH_TOKEN` is set on the server)
- Body (JSON):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "<tool_name>",
    "arguments": { "<key>": "<value>" }
  }
}
```

**Extract the result** with expression: `{{ $json.result.content[0].text }}`

## Exposing n8n publicly (webhooks from external services)

If you need n8n webhooks reachable from the internet (e.g. GitHub webhooks):

1. Set `N8N_HOST` and `N8N_PUBLIC_URL` in `.env` to your public domain.
2. Add `N8N_BASIC_AUTH_ACTIVE=true` and set credentials in `.env`.
3. Put n8n behind a reverse proxy (nginx/Caddy) with TLS.
4. See `terraform/aws/` for an EC2 + Route53 setup that handles this.

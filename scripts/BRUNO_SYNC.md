# Bruno Collection Sync Scripts

Generate a live Bruno collection from your MCP endpoint's tool definitions. Like GraphQL introspection, but for MCP.

## What This Does

These scripts call the MCP `tools/list` endpoint and generate a complete Bruno collection with:
- Individual `.bru` files for each tool, organized by category
- Pre-configured environments (local + production)
- Tool descriptions in the Bruno docs
- Ready-to-import collection structure

## Usage

### Bash Version

```bash
./scripts/sync-bruno-collection.sh <baseUrl> <authToken> [output-dir]
```

**Arguments:**
- `baseUrl`: MCP endpoint URL (default: `https://mcp.chrismarasco.io`)
- `authToken`: Bearer token for authentication (required)
- `output-dir`: Where to save the collection (default: `./bruno-generated`)

**Example:**
```bash
./scripts/sync-bruno-collection.sh https://mcp.chrismarasco.io your-token-here ./bruno-live
```

### Node.js Version

```bash
node scripts/sync-bruno-collection.js <baseUrl> <authToken> [output-dir]
```

**Example:**
```bash
node scripts/sync-bruno-collection.js https://mcp.chrismarasco.io your-token-here ./bruno-live
```

## Getting Your Auth Token

1. **Local development**: No token needed, use default from `.env`
2. **Production (mcp.chrismarasco.io)**:
   - Generate or retrieve your token from the hosted service
   - Use it with the production baseUrl

## Workflow

### One-time Setup
```bash
# Generate collection
./scripts/sync-bruno-collection.sh https://mcp.chrismarasco.io $YOUR_TOKEN ./bruno-live

# Open in Bruno
# 1. Click "Open Collection"
# 2. Select: ./bruno-live
# 3. Update auth token in environments
```

### Keep Collection in Sync
```bash
# Regenerate whenever MCP tools change
./scripts/sync-bruno-collection.sh https://mcp.chrismarasco.io $YOUR_TOKEN ./bruno-live

# Or use as a CI/CD step:
# - Schedule weekly regeneration
# - Commit changes and create PR
# - Or just regenerate on-demand
```

## Output Structure

```
bruno-live/
├── bruno.json
├── environments/
│   ├── local.bru
│   └── production.bru
├── audit/
│   ├── folder.bru
│   ├── approve-execution.bru
│   ├── deny-execution.bru
│   └── ...
├── calendar/
│   ├── folder.bru
│   ├── calendar-create.bru
│   └── ...
└── [other-categories]/
```

## Integration Ideas

### GitHub Actions
```yaml
# .github/workflows/sync-bruno.yml
name: Sync Bruno Collection
on:
  schedule:
    - cron: '0 * * * *'  # hourly
  workflow_dispatch:

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: |
          ./scripts/sync-bruno-collection.sh \
            ${{ secrets.MCP_BASEURL }} \
            ${{ secrets.MCP_TOKEN }} \
            ./bruno-live
      - uses: stefanzweifel/git-auto-commit-action@v4
        with:
          file_pattern: bruno-live/
          commit_message: 'chore: sync bruno collection'
```

### Package.json Script
```json
{
  "scripts": {
    "bruno:sync": "node scripts/sync-bruno-collection.js $MCP_BASEURL $MCP_TOKEN ./bruno-live",
    "bruno:sync:prod": "MCP_BASEURL=https://mcp.chrismarasco.io node scripts/sync-bruno-collection.js $MCP_BASEURL $MCP_TOKEN ./bruno-live"
  }
}
```

## Notes

- The generated collection is **safe to commit** to version control
- Regenerating overwrites existing files (no conflicts)
- Token should be stored in environment variables, not committed
- Empty `arguments: {}` in each request—update with actual params when using

## Troubleshooting

### "Invalid JSON" Error
- Check that `authToken` is correct
- Verify `baseUrl` is accessible
- Ensure the MCP endpoint is running

### Token Expires
- Regenerate the collection with a fresh token
- Update token in Bruno environments

### Too Many Files
- This is normal! MCP exposes 100+ tools
- Use Bruno's search to find specific tools
- Organization by category helps navigate

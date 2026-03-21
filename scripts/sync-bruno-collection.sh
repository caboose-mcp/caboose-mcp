#!/bin/bash

# Sync Bruno collection from live MCP endpoint
# Usage: ./sync-bruno-collection.sh [baseUrl] [authToken] [output-dir]

set -euo pipefail

BASEURL="${1:-https://mcp.chrismarasco.io}"
AUTHTOKEN="${2:-}"
OUTDIR="${3:-./bruno-generated}"

if [[ -z "$AUTHTOKEN" ]]; then
  echo "Usage: $0 <baseUrl> <authToken> [output-dir]"
  echo "  baseUrl: MCP endpoint (default: https://mcp.chrismarasco.io)"
  echo "  authToken: Bearer token for authentication"
  echo "  output-dir: Output directory (default: ./bruno-generated)"
  exit 1
fi

# Create output directory
mkdir -p "$OUTDIR"

echo "Fetching tools from $BASEURL..."

# Call tools/list
RESPONSE=$(curl -s -X POST "$BASEURL/mcp" \
  -H "Authorization: Bearer $AUTHTOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
  }')

if echo "$RESPONSE" | grep -q '"error"'; then
  echo "Error from MCP server:"
  echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
  exit 1
fi

# Extract tools
TOOLS=$(echo "$RESPONSE" | jq -r '.result.tools[]' 2>/dev/null)

if [[ -z "$TOOLS" ]]; then
  echo "No tools found in response"
  exit 1
fi

# Count tools
TOOL_COUNT=$(echo "$RESPONSE" | jq '.result.tools | length')
echo "Found $TOOL_COUNT tools"

# Create directory structure by category
declare -A CATEGORIES
COUNTER=0

echo "$RESPONSE" | jq -r '.result.tools[]' | while read -r TOOL_JSON; do
  COUNTER=$((COUNTER + 1))

  NAME=$(echo "$TOOL_JSON" | jq -r '.name')
  DESC=$(echo "$TOOL_JSON" | jq -r '.description // "No description"' | sed 's/"/\\"/g')
  SCHEMA=$(echo "$TOOL_JSON" | jq '.inputSchema')

  # Extract category from name (first part before underscore)
  CATEGORY=$(echo "$NAME" | cut -d'_' -f1)

  # Create category directory
  mkdir -p "$OUTDIR/$CATEGORY"

  # Generate .bru file
  BRU_FILE="$OUTDIR/$CATEGORY/$NAME.bru"

  cat > "$BRU_FILE" << EOF
meta {
  name: $NAME
  type: http
  seq: $COUNTER
}

post {
  url: {{baseUrl}}/mcp
  body: json
}

body:json {
  {
    "jsonrpc": "2.0",
    "id": $COUNTER,
    "method": "tools/call",
    "params": {
      "name": "$NAME",
      "arguments": {}
    }
  }
}

docs {
  $DESC
}
EOF

  echo "  ✓ Generated: $CATEGORY/$NAME.bru"
done

# Create folder.bru files for each category
for CATEGORY in "$OUTDIR"/*; do
  if [[ -d "$CATEGORY" ]] && [[ $(basename "$CATEGORY") != "environments" ]]; then
    cat > "$CATEGORY/folder.bru" << EOF
meta {
  name: $(basename "$CATEGORY")
  type: http
}
EOF
    echo "  ✓ Created: $(basename "$CATEGORY")/folder.bru"
  fi
done

# Create environments
mkdir -p "$OUTDIR/environments"

cat > "$OUTDIR/environments/local.bru" << EOF
vars {
  baseUrl: http://localhost:8080
  authToken: your-mcp-auth-token-here
}
EOF

cat > "$OUTDIR/environments/production.bru" << EOF
vars {
  baseUrl: https://mcp.chrismarasco.io
  authToken: your-mcp-auth-token-here
}
EOF

echo "  ✓ Created: environments/local.bru"
echo "  ✓ Created: environments/production.bru"

# Create bruno.json
cat > "$OUTDIR/bruno.json" << EOF
{
  "version": "1",
  "name": "caboose-mcp-generated",
  "type": "collection"
}
EOF

echo "  ✓ Created: bruno.json"

echo ""
echo "✅ Collection generated successfully!"
echo "📁 Location: $OUTDIR"
echo ""
echo "To use in Bruno:"
echo "  1. Open Bruno"
echo "  2. Click 'Open Collection'"
echo "  3. Select: $OUTDIR"
echo "  4. Update auth token in environments"

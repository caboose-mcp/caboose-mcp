#!/bin/bash
# setup-discord-bot.sh — Interactive Discord bot setup for fafb

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"
SERVER_DIR="$PROJECT_ROOT/packages/server"
ENV_FILE="$PROJECT_ROOT/.env"

echo "🤖 fafb Discord Bot Setup"
echo "=========================="
echo ""

# Check if .env already exists
if [ -f "$ENV_FILE" ]; then
    echo "⚠️  .env already exists at $ENV_FILE"
    read -p "Do you want to overwrite it? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Cancelled."
        exit 0
    fi
fi

# Get Discord token
echo ""
echo "📋 Enter your Discord bot token"
echo "   (Get it from: https://discord.com/developers/applications)"
echo "   (Bot → Copy TOKEN)"
read -s -p "Discord Bot Token: " DISCORD_TOKEN
echo ""

if [ -z "$DISCORD_TOKEN" ]; then
    echo "❌ Discord token is required"
    exit 1
fi

# Get channel ID
echo ""
echo "📋 Enter your Discord channel ID"
echo "   (Enable Developer Mode: Settings → Advanced → Developer Mode)"
echo "   (Right-click channel → Copy Channel ID)"
read -p "Channel ID: " DISCORD_CHANNEL_ID

if [ -z "$DISCORD_CHANNEL_ID" ]; then
    echo "❌ Channel ID is required"
    exit 1
fi

# Get GitHub orgs
echo ""
echo "📋 Enter GitHub orgs to monitor (comma-separated)"
echo "   (Default: caboose-mcp)"
read -p "GitHub Orgs: " GITHUB_ORGS
GITHUB_ORGS=${GITHUB_ORGS:-caboose-mcp}

# Create .env file
echo ""
echo "📝 Creating .env file..."

cat > "$ENV_FILE" << EOF
# ── GitHub ────────────────────────────────────────────────────────────────────
GITHUB_ORGS=$GITHUB_ORGS

# ── Discord ────────────────────────────────────────────────────────────────────
DISCORD_TOKEN=$DISCORD_TOKEN
DISCORD_BOT_CHANNELS=$DISCORD_CHANNEL_ID

# ── Optional ───────────────────────────────────────────────────────────────────
# DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
EOF

chmod 600 "$ENV_FILE"
echo "✅ Created $ENV_FILE"

# Add to .gitignore
if [ ! -f "$PROJECT_ROOT/.gitignore" ]; then
    echo ".env" > "$PROJECT_ROOT/.gitignore"
    echo "✅ Created .gitignore"
else
    if ! grep -q "^\.env$" "$PROJECT_ROOT/.gitignore"; then
        echo ".env" >> "$PROJECT_ROOT/.gitignore"
        echo "✅ Added .env to .gitignore"
    fi
fi

# Build the binary
echo ""
echo "🔨 Building fafb binary..."
cd "$SERVER_DIR"
go build -o fafb . 2>&1 | grep -v "^#" || true

if [ ! -f "$SERVER_DIR/fafb" ]; then
    echo "❌ Build failed"
    exit 1
fi
echo "✅ Binary built: $SERVER_DIR/fafb"

# Test the bot
echo ""
echo "🧪 Testing bot connection..."
echo ""
echo "Starting bot in 3 seconds (press Ctrl+C to stop test)..."
sleep 3

timeout 5 bash -c "
    cd '$SERVER_DIR'
    export \$(cat ../.env | xargs)
    ./fafb --bots 2>&1 | head -20
" || true

echo ""
echo "✅ Setup complete!"
echo ""
echo "📢 To start the bot:"
echo "   cd $SERVER_DIR"
echo "   export \$(cat ../.env | xargs)"
echo "   ./fafb --bots"
echo ""
echo "💡 Or use this one-liner:"
echo "   cd $SERVER_DIR && export \$(cat ../.env | xargs) && ./fafb --bots"
echo ""
echo "📖 For more info, see: $PROJECT_ROOT/DISCORD_SETUP.md"
echo ""

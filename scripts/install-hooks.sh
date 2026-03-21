#!/bin/bash
# Install Git pre-commit hooks for security scanning
# Run this once: ./scripts/install-hooks.sh

set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOKS_DIR="$REPO_ROOT/.git/hooks"

echo "🔒 Installing git hooks for security scanning..."

# Create .git/hooks directory if it doesn't exist
mkdir -p "$HOOKS_DIR"

# Create pre-commit hook
cat > "$HOOKS_DIR/pre-commit" << 'EOF'
#!/bin/bash
# Pre-commit hook: Security and code quality checks

set -e

REPO_ROOT="$(git rev-parse --show-toplevel)"
GO_FILES=$(git diff --cached --name-only --diff-filter=d | grep -E '\.go$' || true)
NODE_FILES=$(git diff --cached --name-only --diff-filter=d | grep -E '\.(ts|js|json)$' || true)
ENV_FILES=$(git diff --cached --name-only --diff-filter=d | grep -E '\.env' || true)

# Color codes
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
NC='\033[0m'

# 1. Prevent accidental .env commits
if [ -n "$ENV_FILES" ]; then
    if echo "$ENV_FILES" | grep -q "\.env$"; then
        echo -e "${RED}❌ ERROR: Attempting to commit .env file!${NC}"
        echo "   .env files should never be committed."
        echo "   If you need version control for .env, use .env.example instead."
        exit 1
    fi
fi

# 2. Run gosec on Go files
if [ -n "$GO_FILES" ]; then
    if command -v gosec &> /dev/null; then
        echo "🔍 Running gosec on Go files..."
        cd "$REPO_ROOT/packages/server"

        # Run gosec, but don't fail on LOW severity
        if /home/caboose/go/bin/gosec -fmt json ./... 2>/dev/null | \
           jq '.Issues[] | select(.severity=="HIGH" or .severity=="MEDIUM")' | grep -q .; then
            echo -e "${YELLOW}⚠️  gosec found HIGH/MEDIUM severity issues${NC}"
            echo "   Run: /home/caboose/go/bin/gosec ./..."
            echo "   to see details. You can add //nolint:gosec comments if false positives."
            # exit 1  # Uncomment to fail the commit
        else
            echo -e "${GREEN}✅ gosec passed${NC}"
        fi
    fi
fi

# 3. Run npm audit on Node files
if [ -n "$NODE_FILES" ]; then
    if command -v pnpm &> /dev/null; then
        echo "🔍 Running npm audit..."
        cd "$REPO_ROOT"

        # Run npm audit (continue on error to just warn)
        if pnpm audit --prod 2>&1 | grep -q "CRITICAL\|HIGH"; then
            echo -e "${YELLOW}⚠️  npm audit found vulnerabilities${NC}"
            echo "   Run: pnpm audit --prod"
            echo "   to see details and run: pnpm update"
            # exit 1  # Uncomment to fail the commit
        else
            echo -e "${GREEN}✅ npm audit passed${NC}"
        fi
    fi
fi

# 4. Check for obvious secrets
if grep -r "password.*=" --include="*.go" "$REPO_ROOT/packages" 2>/dev/null | \
   grep -v "test" | grep -v "//" | grep -q "=.*['\"]"; then
    echo -e "${YELLOW}⚠️  Potential hardcoded password detected${NC}"
    echo "   Make sure secrets come from environment variables!"
    exit 1
fi

echo -e "${GREEN}✅ Pre-commit checks passed!${NC}"
EOF

chmod +x "$HOOKS_DIR/pre-commit"

echo "✓ Pre-commit hook installed"

# Create commit-msg hook (for conventional commits)
cat > "$HOOKS_DIR/commit-msg" << 'EOF'
#!/bin/bash
# Commit message hook: Enforce conventional commits

COMMIT_MSG=$(cat "$1")

# Only check for commits that have messages (not merges, etc)
if [ -z "$COMMIT_MSG" ]; then
    exit 0
fi

# Check for conventional commit format: type: subject
if ! echo "$COMMIT_MSG" | grep -qE "^(feat|fix|docs|style|refactor|perf|test|chore|security|ci)(\(.+\))?!?: .{1,50}"; then
    echo "❌ Commit message does not follow conventional commits"
    echo "   Format: type(scope): description"
    echo "   Types: feat, fix, docs, style, refactor, perf, test, chore, security, ci"
    echo ""
    echo "   Examples:"
    echo "   - feat: add JWT token validation"
    echo "   - fix(auth): handle expired tokens gracefully"
    echo "   - security: replace weak RNG with crypto/rand"
    exit 1
fi
EOF

chmod +x "$HOOKS_DIR/commit-msg"

echo "✓ Commit-msg hook installed"

# Create post-merge hook (to update dependencies)
cat > "$HOOKS_DIR/post-merge" << 'EOF'
#!/bin/bash
# Post-merge hook: Warn about dependency changes

REPO_ROOT="$(git rev-parse --show-toplevel)"

# Check if Go modules changed
if git diff HEAD@{1} HEAD --name-only | grep -q "go.mod\|go.sum"; then
    echo "⚠️  Go modules changed. Consider running: go mod tidy"
fi

# Check if package.json changed
if git diff HEAD@{1} HEAD --name-only | grep -q "package.json\|pnpm-lock.yaml"; then
    echo "⚠️  Node dependencies changed. Consider running: pnpm install"
fi
EOF

chmod +x "$HOOKS_DIR/post-merge"

echo "✓ Post-merge hook installed"

echo ""
echo "✅ All hooks installed successfully!"
echo ""
echo "📝 Hooks installed:"
echo "   - pre-commit: Runs security checks before committing"
echo "   - commit-msg: Enforces conventional commits"
echo "   - post-merge: Warns about dependency changes"
echo ""
echo "To manually run the full security scan:"
echo "   ./scripts/security-scan.sh"

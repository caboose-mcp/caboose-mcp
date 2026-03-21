#!/bin/bash
# Security scanning script for fafb
# Runs all security checks and generates a report

set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="${REPO_ROOT}/security-results-${TIMESTAMP}"

mkdir -p "$RESULTS_DIR"

echo "🔒 fafb Security Scanner"
echo "================================"
echo "Results directory: $RESULTS_DIR"
echo ""

# Color codes
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Counter for issues
CRITICAL_COUNT=0
HIGH_COUNT=0
MEDIUM_COUNT=0

# ============================================================================
# 1. Go Security Scan (gosec)
# ============================================================================
echo "📋 Running Go security scan (gosec)..."

if ! command -v gosec &> /dev/null; then
    echo "  Installing gosec..."
    go install github.com/securego/gosec/v2/cmd/gosec@latest > /dev/null 2>&1
fi

cd "$REPO_ROOT/packages/server"
/home/caboose/go/bin/gosec -fmt json ./... > "$RESULTS_DIR/gosec.json" 2>/dev/null || true

# Parse gosec results
GOSEC_HIGH=$(jq '[.Issues[] | select(.severity=="HIGH")] | length' "$RESULTS_DIR/gosec.json" 2>/dev/null || echo 0)
GOSEC_MEDIUM=$(jq '[.Issues[] | select(.severity=="MEDIUM")] | length' "$RESULTS_DIR/gosec.json" 2>/dev/null || echo 0)

echo "  ✓ Found $GOSEC_HIGH HIGH, $GOSEC_MEDIUM MEDIUM severity issues"
HIGH_COUNT=$((HIGH_COUNT + GOSEC_HIGH))
MEDIUM_COUNT=$((MEDIUM_COUNT + GOSEC_MEDIUM))

# ============================================================================
# 2. Node.js Dependency Audit
# ============================================================================
echo "📋 Running Node.js dependency audit (pnpm)..."

cd "$REPO_ROOT"
pnpm audit --json > "$RESULTS_DIR/npm-audit.json" 2>/dev/null || true

# Parse npm audit results
NPM_CRITICAL=$(jq '.metadata.vulnerabilities.critical // 0' "$RESULTS_DIR/npm-audit.json" 2>/dev/null || echo 0)
NPM_HIGH=$(jq '.metadata.vulnerabilities.high // 0' "$RESULTS_DIR/npm-audit.json" 2>/dev/null || echo 0)
NPM_MEDIUM=$(jq '.metadata.vulnerabilities.medium // 0' "$RESULTS_DIR/npm-audit.json" 2>/dev/null || echo 0)

echo "  ✓ Found $NPM_CRITICAL CRITICAL, $NPM_HIGH HIGH, $NPM_MEDIUM MEDIUM vulnerabilities"
CRITICAL_COUNT=$((CRITICAL_COUNT + NPM_CRITICAL))
HIGH_COUNT=$((HIGH_COUNT + NPM_HIGH))
MEDIUM_COUNT=$((MEDIUM_COUNT + NPM_MEDIUM))

# ============================================================================
# 3. Secret Scanning
# ============================================================================
echo "📋 Scanning for exposed secrets..."

cd "$REPO_ROOT"

# Check for common secret patterns (not in .env files)
SECRETS_FOUND=$(grep -r "password\|secret\|token\|key" \
    --include="*.go" --include="*.ts" --include="*.js" \
    packages/ 2>/dev/null | \
    grep -v "test" | grep -v "//" | grep -v "example" | wc -l || echo 0)

# Detailed scan for actual secrets (not just references)
grep -r "password.*=.*['\"]" packages/ --include="*.go" 2>/dev/null > "$RESULTS_DIR/potential-secrets.txt" || true
grep -r "PRIVATE_KEY.*=.*" packages/ --include="*.go" 2>/dev/null >> "$RESULTS_DIR/potential-secrets.txt" || true

ACTUAL_SECRETS=$(wc -l < "$RESULTS_DIR/potential-secrets.txt")
if [ "$ACTUAL_SECRETS" -gt 0 ]; then
    echo "  ⚠️  Found $ACTUAL_SECRETS potential hardcoded secrets"
    HIGH_COUNT=$((HIGH_COUNT + 1))
fi

echo "  ✓ Scanned for exposed credentials"

# ============================================================================
# 4. File Permissions Check
# ============================================================================
echo "📋 Checking file permissions..."

PERMS_ISSUES=0

# Check .env files
for env_file in .env .env.example packages/server/.env packages/server/.env.example; do
    if [ -f "$env_file" ]; then
        perms=$(stat -c '%a' "$env_file" 2>/dev/null || stat -f '%OLp' "$env_file" | tail -c 4)
        if [[ ! "$perms" =~ ^[46]00$ ]]; then
            echo "  ⚠️  $env_file has permissions $perms (should be 600 or 400)"
            PERMS_ISSUES=$((PERMS_ISSUES + 1))
            MEDIUM_COUNT=$((MEDIUM_COUNT + 1))
        fi
    fi
done

if [ $PERMS_ISSUES -eq 0 ]; then
    echo "  ✓ File permissions are correct"
fi

# ============================================================================
# 5. Dependency Outdated Check
# ============================================================================
echo "📋 Checking for outdated dependencies..."

cd "$REPO_ROOT/packages/server"
go list -u -json ./... 2>/dev/null | jq -s '[.[] | select(.Update != null)] | length' > "$RESULTS_DIR/go-outdated.txt" 2>/dev/null || echo 0 > "$RESULTS_DIR/go-outdated.txt"

OUTDATED_GO=$(cat "$RESULTS_DIR/go-outdated.txt")
if [ "$OUTDATED_GO" -gt 0 ]; then
    echo "  ⚠️  $OUTDATED_GO Go dependencies have updates available"
fi

cd "$REPO_ROOT"
pnpm outdated --json > "$RESULTS_DIR/npm-outdated.json" 2>/dev/null || true

echo "  ✓ Dependency check complete"

# ============================================================================
# 6. Generate Summary Report
# ============================================================================
echo ""
echo "================================"
echo "📊 SECURITY SCAN SUMMARY"
echo "================================"
echo ""

total_issues=$((CRITICAL_COUNT + HIGH_COUNT + MEDIUM_COUNT))

if [ $CRITICAL_COUNT -gt 0 ]; then
    echo -e "${RED}🔴 CRITICAL: $CRITICAL_COUNT${NC}"
fi

if [ $HIGH_COUNT -gt 0 ]; then
    echo -e "${RED}🔴 HIGH: $HIGH_COUNT${NC}"
fi

if [ $MEDIUM_COUNT -gt 0 ]; then
    echo -e "${YELLOW}🟡 MEDIUM: $MEDIUM_COUNT${NC}"
fi

echo ""

if [ $total_issues -eq 0 ]; then
    echo -e "${GREEN}✅ No security issues found!${NC}"
else
    echo -e "${RED}⚠️  Found $total_issues total issues${NC}"
    echo ""
    echo "Detailed results:"
    echo "  - GoSec report: $RESULTS_DIR/gosec.json"
    echo "  - npm audit: $RESULTS_DIR/npm-audit.json"
    echo "  - Potential secrets: $RESULTS_DIR/potential-secrets.txt"
    echo "  - Outdated Go deps: $RESULTS_DIR/go-outdated.txt"
fi

echo ""
echo "Full results saved to: $RESULTS_DIR"
echo ""

# ============================================================================
# 7. Create HTML Report (if pandoc available)
# ============================================================================
if command -v pandoc &> /dev/null; then
    echo "📄 Generating HTML report..."
    # Optional: Convert markdown report to HTML
fi

# Exit with error if critical/high issues found
if [ $CRITICAL_COUNT -gt 0 ] || [ $HIGH_COUNT -gt 0 ]; then
    exit 1
fi

exit 0

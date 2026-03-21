# Security Setup Guide

This guide will help you set up automated security scanning for caboose-mcp.

## Quick Start (5 minutes)

### 1. **Fix Environment File Permissions**
```bash
cd /home/caboose/dev/caboose-mcp
chmod 600 .env
chmod 600 packages/server/.env.example
```

### 2. **Update Dependencies**
```bash
# Fix esbuild vulnerability
pnpm update esbuild@^0.25.0
pnpm audit --fix

# Update Go modules
cd packages/server
go get -u ./...
go mod tidy
```

### 3. **Install Security Hooks**
```bash
cd /home/caboose/dev/caboose-mcp
./scripts/install-hooks.sh
```

### 4. **Run First Security Scan**
```bash
./scripts/security-scan.sh
```

## Detailed Setup

### Option A: Local Development (Recommended)

#### Install Required Tools
```bash
# Install gosec (already done)
go install github.com/securego/gosec/v2/cmd/gosec@latest

# Install git-secrets (prevent secret commits)
git clone https://github.com/awslabs/git-secrets.git
cd git-secrets
make install
cd ..
rm -rf git-secrets

# Initialize git-secrets for this repo
git secrets --install
git secrets --register-aws

# Add custom patterns (optional)
git secrets --add '(?i)password\s*[:=]\s*["\'].*["\']'
git secrets --add 'PRIVATE_KEY|API_KEY|SECRET_KEY'
```

#### Enable Pre-commit Hooks
```bash
cd /home/caboose/dev/caboose-mcp
./scripts/install-hooks.sh
```

#### Verify Setup
```bash
# Try to commit a test .env change (should be blocked)
echo "TEST=value" >> .env
git add .env
git commit -m "test: check .env blocking"
# Should fail with: "❌ ERROR: Attempting to commit .env file!"

# Restore .env
git checkout .env
```

### Option B: CI/CD Integration (GitHub Actions)

The `.github/workflows/security-scan.yml` file is already set up. It will:

1. **Run on every push and PR** to main/develop branches
2. **Run daily at 2 AM UTC** for continuous monitoring
3. **Scan with:**
   - GoSec (Go security)
   - npm audit (Node.js dependencies)
   - Trivy (container/filesystem scanning)
   - TruffleHog (secret detection)

**Enable in GitHub:**
1. Push the workflow file: `git push origin`
2. Go to: Repository → Settings → Security → Code security and analysis
3. Enable "GitHub Advanced Security" and "Secret scanning"

**View Results:**
- GitHub → Security → Code scanning alerts
- GitHub → Security → Secret scanning
- GitHub → Actions → Security Scan (workflow runs)

## Running Security Scans

### Manual Full Scan
```bash
./scripts/security-scan.sh
```

Output:
- Results saved to `security-results-YYYYMMDD_HHMMSS/`
- Includes: gosec.json, npm-audit.json, potential-secrets.txt

### Individual Scans

**Go Security:**
```bash
cd packages/server
/home/caboose/go/bin/gosec -fmt json ./...
```

**Node.js Audit:**
```bash
pnpm audit --prod
pnpm audit --fix
```

**Check for secrets:**
```bash
git secrets --scan
# or manually
grep -r "password\|secret\|token\|key" --include="*.go" packages/ | \
  grep -v "test" | grep -v "//"
```

**List outdated dependencies:**
```bash
go list -u -m all

cd ../packages/vscode-extension
pnpm outdated
```

## Fixing Security Issues

### Issue: TLS InsecureSkipVerify (printing.go)
```bash
# Edit the file
nano packages/server/tools/printing.go

# Fix: Replace InsecureSkipVerify with certificate pinning
# See: SECURITY_AUDIT_REPORT.md for details
```

### Issue: Unhandled JSON Errors
```bash
# Find all instances
grep -n "json.Unmarshal" packages/server/tools/cloudsync.go

# Fix: Add error checking
# Old: json.Unmarshal(data, &m)
# New: if err := json.Unmarshal(data, &m); err != nil { ... }
```

### Issue: Weak Random Number Generation
```bash
# File: packages/server/tools/jokes.go
# Fix: Use crypto/rand instead of math/rand
# See: SECURITY_AUDIT_REPORT.md for code example
```

## Ignoring False Positives

### Suppress GoSec warnings
```go
var cmd *exec.Cmd
cmd.Output() //nolint:gosec  // False positive: input validated above
```

### Suppress git-secrets warnings
```bash
# For a specific line/pattern
git secrets --add -a "pattern-that-is-not-a-secret"

# Or in a commit
git commit --no-verify -m "chore: add example config"
```

## Monitoring & Alerts

### Slack Notifications (Optional)
The GitHub Actions workflow can notify Slack on failures.

**Setup:**
1. Create Slack webhook: https://api.slack.com/messaging/webhooks
2. Add to GitHub Secrets:
   - Go to: Settings → Secrets → New repository secret
   - Name: `SLACK_WEBHOOK_URL`
   - Value: Your webhook URL

### Email Alerts
GitHub will email you automatically for:
- Failed security scans
- Secret scanning alerts
- Dependency vulnerability alerts

## Troubleshooting

### `gosec not found`
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
export PATH="$HOME/go/bin:$PATH"
```

### `pnpm audit` failures don't block commits
The pre-commit hook is set to warn but not fail. To make it strict:
```bash
# Edit .git/hooks/pre-commit
# Uncomment: # exit 1
```

### Permission denied when running scripts
```bash
chmod +x scripts/security-scan.sh
chmod +x scripts/install-hooks.sh
```

## Additional Resources

- [SECURITY_AUDIT_REPORT.md](./SECURITY_AUDIT_REPORT.md) - Detailed findings
- [GoSec Documentation](https://github.com/securego/gosec)
- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [Go Security Guidelines](https://go.dev/security/)

## Maintenance Schedule

- **Weekly:** Review GitHub security alerts
- **Monthly:** Run full security audit and update dependencies
- **Quarterly:** Penetration testing / security review

## Questions?

If you have questions about security findings:
1. Check [SECURITY_AUDIT_REPORT.md](./SECURITY_AUDIT_REPORT.md) for details
2. Review the specific file/line mentioned in the report
3. Look up the CWE reference at https://cwe.mitre.org/

---

**Last Updated:** 2026-03-20

# fafb Security Audit Report

**Generated:** 2026-03-20
**Scope:** Static code analysis + configuration review
**Tools Used:** gosec, npm audit, manual inspection

---

## Executive Summary

Your fafb server has **good foundational security practices**, but contains **several actionable security concerns** that should be addressed:

- ✅ **Strong:** Auth middleware, CORS controls, JWT-based access control
- ⚠️ **Medium:** Weak RNG usage, unhandled errors, TLS verification bypass
- ⚠️ **High:** Command execution patterns (exec.Command), potential for issues if user input flows through

**Overall Risk Level:** **MEDIUM** (mostly low-severity issues, no critical vulnerabilities detected)

---

## Critical & High-Severity Findings

### 1. **[HIGH] TLS InsecureSkipVerify on Bambu Printer Connection**
**Location:** `packages/server/tools/printing.go:103`
**Severity:** HIGH
**Issue:**
```go
opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
```

**Risk:** This disables certificate validation, making connections vulnerable to MITM attacks.

**Recommendation:**
- If Bambu printer uses self-signed certs, implement certificate pinning instead
- Load the cert bundle and validate against it
- Document why this is necessary in a comment

**Fix:**
```go
// Load Bambu's self-signed cert
certs, err := os.ReadFile(filepath.Join(cfg.ClaudeDir, "bambu-ca.crt"))
if err != nil {
    return fmt.Errorf("failed to load Bambu CA cert: %w", err)
}

pool := x509.NewCertPool()
pool.AppendCertsFromPEM(certs)

opts.SetTLSConfig(&tls.Config{
    RootCAs: pool,
})
```

---

### 2. **[HIGH] Weak Random Number Generation**
**Location:** `packages/server/tools/jokes.go:52, 76, 116`
**Severity:** HIGH (for crypto/security contexts)
**Issue:**
```go
r := rand.New(rand.NewSource(time.Now().UnixNano()))
selectedJoke := jokes[r.Intn(len(jokes))]
```

**Risk:** While non-critical for joke selection, demonstrates weak RNG pattern. If this pattern spreads to token generation or crypto, it's catastrophic.

**Recommendation:** Use `crypto/rand` for all random operations:
```go
import "crypto/rand"

n, err := rand.Int(rand.Reader, big.NewInt(int64(len(jokes))))
if err != nil {
    return nil, err
}
selectedJoke := jokes[n.Int64()]
```

---

## Medium-Severity Findings

### 3. **[MEDIUM] Unhandled Errors in JSON Unmarshaling**
**Location:** `packages/server/tools/cloudsync.go` (multiple: lines 149, 280, 288, 292, etc.)
**Severity:** MEDIUM
**Issue:**
```go
var m map[string]string
json.Unmarshal(data, &m)  // Error ignored!
```

**Risk:** Silent failures can lead to unpredictable behavior. Attackers could send malformed JSON and cause the server to silently skip critical operations.

**Recommendation:** Check all errors:
```go
if err := json.Unmarshal(data, &m); err != nil {
    log.Printf("error parsing JSON: %v", err)
    return fmt.Errorf("invalid JSON: %w", err)
}
```

**Action Items:**
- Run: `grep -r "json.Unmarshal" packages/server/tools/cloudsync.go` and fix ~12 instances
- Same pattern in `tools/database.go` and other files

---

### 4. **[MEDIUM] Command Execution Patterns**
**Location:** `packages/server/tools/cloudsync.go` (exec.Command)
**Severity:** MEDIUM (currently safe, but high-risk pattern)
**Issue:**
```go
exec.Command("aws", "s3", "cp", localFile, s3Path).CombinedOutput()
```

**Current Status:** ✅ Safe — arguments are not user-controlled
**Risk:** If any of these arguments come from user input, command injection is trivial.

**Recommendation:**
- Document that these paths come from internal config only
- Add input validation at entry points
- Add tests to verify no user input reaches exec.Command

---

### 5. **[MEDIUM] Missing Error Handling in AWS S3 Operations**
**Location:** `packages/server/tools/cloudsync.go:495, 532-536`
**Severity:** MEDIUM
**Issue:**
```go
exec.Command("aws", "s3api", "put-public-access-block", ...).Run()  // Error ignored!
```

**Risk:** S3 bucket security configurations failing silently.

**Recommendation:**
```go
if err := exec.Command("aws", "s3api", "put-public-access-block", ...).Run(); err != nil {
    log.Printf("warning: failed to set public access block: %v", err)
    // Consider returning error or alerting
}
```

---

## Low-Severity Findings

### 6. **[LOW] Potential Hardcoded Credentials in Example Code**
**Location:** `packages/server/tui/wizard.go:120`
**Severity:** LOW (example text only)
**Issue:**
```go
{key: "POSTGRES_URL", label: "PostgreSQL URL",
 description: "postgres://user:pass@host:5432/dbname", ...}
```

**Risk:** Users might copy the example format with their actual password.

**Recommendation:** Use placeholder syntax:
```
description: "postgres://user@host:5432/dbname (password in env var PGPASSWORD)"
```

---

### 7. **[LOW] Exposed Debug Logging**
**Location:** `packages/server/tools/slack_gateway.go`
**Issue:**
```go
socketmode.OptionDebug(true),
log.Printf("[slack debug] event type: %s", evt.Type)
```

**Risk:** Debug logs in production could leak sensitive data.

**Recommendation:** Gate behind environment variable:
```go
debug := os.Getenv("DEBUG") == "true"
socketmode.OptionDebug(debug),
```

---

## Architecture & Configuration Review

### ✅ **Authentication: STRONG**
- JWT-based access control with claims injection
- Bearer token validation on protected endpoints
- Admin token bypass with full access
- Magic link exchange for token creation
- WWW-Authenticate headers for discovery

### ✅ **CORS: WELL-CONFIGURED**
- Explicit origin control (not `*`)
- Proper preflight handling
- Limited to necessary methods (GET, POST, OPTIONS)

### ⚠️ **Secrets Management: ADEQUATE**
- `.env` properly gitignored
- Environment variables used for all secrets
- GPG integration for encrypting stored secrets
- **Improvement:** Consider external secrets management (AWS Secrets Manager, HashiCorp Vault)

### ⚠️ **Error Handling: INCONSISTENT**
- Some operations check errors
- Many `os.*` and `json.Unmarshal` calls ignore errors
- Could mask attacks or misconfigurations

---

## Dependency Security

### Node.js Vulnerabilities
**1 MODERATE vulnerability found:**

| Package | Issue | Version | Fix |
|---------|-------|---------|-----|
| esbuild | CORS/CSRF via dev server | ≤0.24.2 | Upgrade to ≥0.25.0 |

**Action:**
```bash
cd /home/caboose/dev/fafb
pnpm update esbuild@^0.25.0
pnpm audit  # Verify fix
```

### Go Dependencies
**Status:** ✅ All major dependencies up-to-date
- No known CVEs in direct dependencies
- Indirect deps (crypto, http) are current

---

## Misconfigurations Detected

### 1. **Environment File Permissions**
```bash
ls -la .env
# Output: -rw-r--r-- 1 caboose caboose 268
```

**Issue:** `.env` is world-readable if secrets are present
**Fix:**
```bash
chmod 600 .env
chmod 600 packages/server/.env.example  # Prevent accidental copy
```

### 2. **No Secret Scanning in CI/CD**
**Current:** No pre-commit hooks or CI checks
**Recommendation:** Add git-secrets or truffleHog

### 3. **Debug Mode in Production**
Ensure `DEBUG=false` or unset in production:
```bash
# Add to startup:
export DEBUG=false
export LOG_LEVEL=info
```

---

## Recommended Security Improvements (Priority Order)

### **IMMEDIATE (Do This Now)**
- [ ] Fix TLS InsecureSkipVerify on Bambu printer (printing.go:103)
- [ ] Add error handling to all `json.Unmarshal` calls
- [ ] Fix file permissions: `chmod 600 .env`
- [ ] Update esbuild dependency: `pnpm update esbuild@^0.25.0`

### **SHORT-TERM (This Week)**
- [ ] Replace weak RNG with crypto/rand (jokes.go)
- [ ] Add input validation guards around exec.Command
- [ ] Implement secret scanning in pre-commit hooks
- [ ] Gate debug logging behind environment variable

### **MEDIUM-TERM (This Sprint)**
- [ ] Add integration with AWS Secrets Manager or HashiCorp Vault
- [ ] Implement audit logging for all auth events
- [ ] Add rate limiting to /auth/verify endpoint
- [ ] Security testing in CI/CD (gosec, trivy, npm audit)

### **LONG-TERM (Roadmap)**
- [ ] Implement SBOM generation (CycloneDX)
- [ ] Regular dependency updates (Dependabot)
- [ ] Penetration testing of OAuth flows
- [ ] Security training for contributors

---

## Automated Security Scanning Setup

### 1. **Pre-commit Hook** (Prevents accidental secret commits)
Create `.git/hooks/pre-commit`:
```bash
#!/bin/bash
# Scan for secrets
if command -v git-secrets &> /dev/null; then
    git secrets --scan
fi

# Run gosec on Go files
if git diff --cached --name-only | grep -q "\.go$"; then
    /home/caboose/go/bin/gosec ./packages/server/... || exit 1
fi

# Run npm audit on Node files
if git diff --cached --name-only | grep -q "packages/vscode-extension"; then
    cd packages/vscode-extension
    npm audit --audit-level=moderate || exit 1
fi
```

Make executable:
```bash
chmod +x .git/hooks/pre-commit
```

### 2. **CI/CD Integration** (GitHub Actions)
Create `.github/workflows/security.yml`:
```yaml
name: Security Scan
on: [push, pull_request]
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run gosec
        run: |
          go install github.com/securego/gosec/v2/cmd/gosec@latest
          gosec -fmt sarif -out results.sarif ./packages/server/...

      - name: Upload SARIF
        uses: github/codeql-action/upload-sarif@v2
        with:
          sarif_file: results.sarif

      - name: npm audit
        run: |
          cd packages/vscode-extension
          npm audit --audit-level=moderate

      - name: Trivy scan
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: 'fs'
          scan-ref: '.'
          format: 'sarif'
          output: 'trivy-results.sarif'
```

### 3. **Local Scanning Commands**
```bash
# Full security audit
cd /home/caboose/dev/fafb

# Go security
echo "=== Go Security Scan ==="
/home/caboose/go/bin/gosec -fmt=json ./packages/server/... | \
  jq '.Issues[] | select(.severity=="HIGH" or .severity=="MEDIUM")'

# Node dependencies
echo "=== Node Dependency Audit ==="
pnpm audit --prod

# Check for secrets
echo "=== Secret Scan ==="
grep -r "password\|secret\|token\|key" --include="*.go" --include="*.ts" \
  packages/ | grep -v "test" | grep -v "//" | head -20
```

---

## Testing & Verification

### Unit Tests to Add
```bash
# Test auth middleware
go test ./... -run TestAuthMiddleware -v

# Test JWT validation
go test ./... -run TestJWT -v

# Test CORS headers
go test ./... -run TestCORS -v
```

### Manual Penetration Testing
```bash
# 1. Test auth bypass
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"method":"tools/list"}'  # Should return 401 if auth required

# 2. Test invalid JWT
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer invalid.jwt.token" \
  -d '{"method":"tools/list"}'  # Should return 401

# 3. Test tool ACL
# (Requires JWT with limited tools list)
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer <limited-jwt>" \
  -d '{"method":"tools/call","params":{"name":"forbidden_tool"}}'  # Should be rejected
```

---

## Compliance Notes

- ✅ No hardcoded credentials in repository
- ✅ Secrets properly managed via environment variables
- ✅ HTTPS/TLS ready (but verify in deployment)
- ✅ Input validation present on MCP endpoint
- ⚠️ Audit logging could be enhanced
- ⚠️ Rate limiting not implemented

---

## Resources & References

- [OWASP Top 10 - 2023](https://owasp.org/www-project-top-ten/)
- [Go Security Best Practices](https://go.dev/security/)
- [gosec Documentation](https://github.com/securego/gosec)
- [JWT Best Practices](https://auth0.com/blog/critical-vulnerabilities-in-json-web-token-libraries/)

---

## Next Steps

1. **Immediately:** Review CRITICAL findings (TLS, JSON errors)
2. **This week:** Implement pre-commit hooks and CI security checks
3. **This sprint:** Add comprehensive error handling
4. **Ongoing:** Keep dependencies updated, monitor security advisories

For questions or clarifications, refer to the specific line numbers and file paths in each finding.


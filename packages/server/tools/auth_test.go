package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testClaudeDir creates a temporary directory for test storage.
func testClaudeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "caboose-auth-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestLoadOrCreateJWTSecret(t *testing.T) {
	dir := testClaudeDir(t)

	secret, err := loadOrCreateJWTSecret(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(secret) != 32 {
		t.Fatalf("expected 32-byte secret, got %d bytes", len(secret))
	}

	// Second load should return the same secret.
	secret2, err := loadOrCreateJWTSecret(dir)
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}
	if string(secret) != string(secret2) {
		t.Fatal("secret changed between loads")
	}

	// File should exist at expected path.
	if _, err := os.Stat(jwtSecretPath(dir)); err != nil {
		t.Fatalf("secret file not created: %v", err)
	}
}

func TestIssuedTokenCRUD(t *testing.T) {
	dir := testClaudeDir(t)

	tokens := loadIssuedTokens(dir)
	if len(tokens) != 0 {
		t.Fatalf("expected empty store, got %d tokens", len(tokens))
	}

	tok := IssuedToken{
		JTI:       "test-jti-1",
		Label:     "test",
		Tools:     []string{"note_add", "focus_start"},
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := saveIssuedTokens(dir, []IssuedToken{tok}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, found := getIssuedTokenByJTI(dir, "test-jti-1")
	if !found {
		t.Fatal("token not found after save")
	}
	if loaded.Label != "test" {
		t.Fatalf("unexpected label: %q", loaded.Label)
	}
	if len(loaded.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(loaded.Tools))
	}

	_, found = getIssuedTokenByJTI(dir, "nonexistent")
	if found {
		t.Fatal("expected not found for unknown JTI")
	}
}

func TestIssueAndVerifyJWT(t *testing.T) {
	dir := testClaudeDir(t)
	secret, _ := loadOrCreateJWTSecret(dir)

	tok := &IssuedToken{
		JTI:          "verify-jti",
		Label:        "alice",
		Tools:        []string{"calendar_list", "note_add"},
		GoogleScopes: []string{"https://www.googleapis.com/auth/calendar.readonly"},
		IssuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}
	_ = saveIssuedTokens(dir, []IssuedToken{*tok})

	tokenStr, err := issueJWT(secret, tok)
	if err != nil {
		t.Fatalf("issueJWT failed: %v", err)
	}
	if !strings.HasPrefix(tokenStr, "eyJ") {
		t.Fatalf("JWT should start with eyJ, got %q", tokenStr[:10])
	}

	claims, err := VerifyJWT(dir, secret, tokenStr)
	if err != nil {
		t.Fatalf("VerifyJWT failed: %v", err)
	}
	if claims.JTI != "verify-jti" {
		t.Fatalf("unexpected JTI: %q", claims.JTI)
	}
	if claims.Subject != "alice" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}
	if len(claims.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(claims.Tools))
	}
}

func TestVerifyJWT_RevokedToken(t *testing.T) {
	dir := testClaudeDir(t)
	secret, _ := loadOrCreateJWTSecret(dir)

	tok := &IssuedToken{
		JTI:       "revoke-jti",
		Label:     "bob",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Revoked:   true,
	}
	_ = saveIssuedTokens(dir, []IssuedToken{*tok})

	tokenStr, _ := issueJWT(secret, tok)
	_, err := VerifyJWT(dir, secret, tokenStr)
	if err == nil {
		t.Fatal("expected error for revoked token")
	}
}

func TestVerifyJWT_WrongSecret(t *testing.T) {
	dir := testClaudeDir(t)
	secret, _ := loadOrCreateJWTSecret(dir)

	tok := &IssuedToken{
		JTI:       "wrong-secret-jti",
		Label:     "charlie",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	_ = saveIssuedTokens(dir, []IssuedToken{*tok})

	tokenStr, _ := issueJWT(secret, tok)

	wrongSecret := make([]byte, 32)
	_, err := VerifyJWT(dir, wrongSecret, tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestMagicTokenCreateAndConsume(t *testing.T) {
	dir := testClaudeDir(t)

	issued := &IssuedToken{
		JTI:       "magic-jti",
		Label:     "magic-test",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	_ = saveIssuedTokens(dir, []IssuedToken{*issued})

	rawToken, err := newMagicToken(dir, "magic-jti")
	if err != nil {
		t.Fatalf("newMagicToken failed: %v", err)
	}
	if !strings.Contains(rawToken, ":magic-jti") {
		t.Fatalf("raw token should contain JTI, got %q", rawToken)
	}

	// Token should be findable in the store.
	magics := loadMagicTokens(dir)
	if len(magics) != 1 {
		t.Fatalf("expected 1 magic token, got %d", len(magics))
	}
	if magics[0].Token != rawToken {
		t.Fatalf("stored token mismatch")
	}
}

func TestIdentityStore(t *testing.T) {
	dir := testClaudeDir(t)

	// Empty store
	m := loadIdentities(dir)
	if len(m) != 0 {
		t.Fatalf("expected empty identities, got %d", len(m))
	}

	m["discord:123"] = "jti-abc"
	m["slack:U456"] = "jti-abc"
	_ = saveIdentities(dir, m)

	jti, ok := LookupIdentity(dir, "discord:123")
	if !ok || jti != "jti-abc" {
		t.Fatalf("expected jti-abc, got %q (ok=%v)", jti, ok)
	}

	_, ok = LookupIdentity(dir, "discord:999")
	if ok {
		t.Fatal("expected not found for unknown identity")
	}
}

func TestClaimsForIdentity(t *testing.T) {
	dir := testClaudeDir(t)

	tok := IssuedToken{
		JTI:          "identity-jti",
		Label:        "linked-user",
		Tools:        []string{"note_add"},
		GoogleScopes: nil,
		IssuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}
	_ = saveIssuedTokens(dir, []IssuedToken{tok})

	ids := map[string]string{"discord:777": "identity-jti"}
	_ = saveIdentities(dir, ids)

	claims, ok := ClaimsForIdentity(dir, "discord:777")
	if !ok {
		t.Fatal("expected claims for linked identity")
	}
	if claims.JTI != "identity-jti" {
		t.Fatalf("unexpected JTI: %q", claims.JTI)
	}
	if claims.Subject != "linked-user" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}

	_, ok = ClaimsForIdentity(dir, "discord:999")
	if ok {
		t.Fatal("expected no claims for unlinked identity")
	}
}

func TestWithAuthClaims_GetAuthClaims(t *testing.T) {
	ctx := context.Background()

	if GetAuthClaims(ctx) != nil {
		t.Fatal("expected nil claims on plain context")
	}

	claims := &JWTClaims{JTI: "ctx-jti", Subject: "ctx-user"}
	ctx2 := WithAuthClaims(ctx, claims)

	got := GetAuthClaims(ctx2)
	if got == nil {
		t.Fatal("expected claims in context")
	}
	if got.JTI != "ctx-jti" {
		t.Fatalf("unexpected JTI: %q", got.JTI)
	}

	// Original context unaffected.
	if GetAuthClaims(ctx) != nil {
		t.Fatal("original context should still be nil")
	}
}

func TestExpandGoogleScope(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"calendar", "https://www.googleapis.com/auth/calendar.readonly"},
		{"calendar.full", "https://www.googleapis.com/auth/calendar"},
		{"https://www.googleapis.com/auth/drive", "https://www.googleapis.com/auth/drive"},
		{"drive.readonly", "https://www.googleapis.com/auth/drive.readonly"},
	}
	for _, c := range cases {
		got := expandGoogleScope(c.in)
		if got != c.want {
			t.Errorf("expandGoogleScope(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV("a, b, , c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("splitCSV result unexpected: %v", got)
	}
	if len(splitCSV("")) != 0 {
		t.Fatal("splitCSV of empty string should return nil/empty")
	}
}

func TestCreateIssuedToken(t *testing.T) {
	dir := testClaudeDir(t)

	issued, magicStr, err := createIssuedToken(dir, "e2e-user", []string{"note_add"}, nil, 30)
	if err != nil {
		t.Fatalf("createIssuedToken failed: %v", err)
	}
	if issued.JTI == "" {
		t.Fatal("expected non-empty JTI")
	}
	if !strings.Contains(magicStr, issued.JTI) {
		t.Fatalf("magic token %q should contain JTI %q", magicStr, issued.JTI)
	}
	if issued.Label != "e2e-user" {
		t.Fatalf("unexpected label: %q", issued.Label)
	}
	if issued.ExpiresAt.Before(time.Now().Add(29 * 24 * time.Hour)) {
		t.Fatal("expiry should be ~30 days out")
	}

	// Verify token was persisted.
	loaded, found := getIssuedTokenByJTI(dir, issued.JTI)
	if !found {
		t.Fatal("token not found after create")
	}
	if loaded.Label != "e2e-user" {
		t.Fatalf("persisted label mismatch: %q", loaded.Label)
	}

	// Magic token file should exist.
	magicFile := filepath.Join(dir, "auth", "magic-tokens.json")
	if _, err := os.Stat(magicFile); err != nil {
		t.Fatalf("magic tokens file not created: %v", err)
	}
}

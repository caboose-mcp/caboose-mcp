package tools

// auth.go — JWT RBAC + Magic Link authentication for caboose-mcp.
//
// Per-token access control: each JWT carries a tool allowlist and Google OAuth
// scope list. The static MCP_AUTH_TOKEN remains as a superuser bypass.
//
// Storage layout:
//   ~/.claude/auth/jwt-secret.key       — 32-byte hex HS256 signing key (auto-created)
//   ~/.claude/auth/issued-tokens.json   — [{jti, label, tools[], google_scopes[], issued_at, expires_at, revoked}]
//   ~/.claude/auth/magic-tokens.json    — [{token, label, tools[], google_scopes[], expires_at}] (15-min one-time)
//   ~/.claude/auth/identities.json      — {"discord:123":"jti-abc", "slack:U456":"jti-abc"}
//
// HTTP endpoint (served by authMiddleware in main package):
//   GET /auth/verify?token=<magic>  →  {"token":"eyJ...","jti":"...","expires_at":"..."}

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// authClaimsKeyType is the unexported context key type for JWT claims.
type authClaimsKeyType struct{}

var authClaimsKey = authClaimsKeyType{}

// JWTClaims holds per-token access control data injected into context.
type JWTClaims struct {
	Subject      string   `json:"sub"`
	JTI          string   `json:"jti"`
	Tools        []string `json:"tools"`
	GoogleScopes []string `json:"google_scopes"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`
}

// GetAuthClaims retrieves JWT claims from context. Returns nil for admin/unauthenticated.
func GetAuthClaims(ctx context.Context) *JWTClaims {
	v, _ := ctx.Value(authClaimsKey).(*JWTClaims)
	return v
}

// WithAuthClaims injects JWT claims into a context. Used by the auth middleware.
func WithAuthClaims(ctx context.Context, claims *JWTClaims) context.Context {
	return context.WithValue(ctx, authClaimsKey, claims)
}

// ---- Storage types ----

// IssuedToken is one entry in issued-tokens.json.
type IssuedToken struct {
	JTI          string    `json:"jti"`
	Label        string    `json:"label"`
	Tools        []string  `json:"tools"`
	GoogleScopes []string  `json:"google_scopes"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Revoked      bool      `json:"revoked"`
}

// magicToken is one entry in magic-tokens.json.
// Token field format: "<16-byte hex>:<jti>" — consumed on first use.
type magicToken struct {
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ---- File paths ----

func authDir(claudeDir string) string {
	return filepath.Join(claudeDir, "auth")
}

func jwtSecretPath(claudeDir string) string {
	return filepath.Join(authDir(claudeDir), "jwt-secret.key")
}

func issuedTokensPath(claudeDir string) string {
	return filepath.Join(authDir(claudeDir), "issued-tokens.json")
}

func magicTokensPath(claudeDir string) string {
	return filepath.Join(authDir(claudeDir), "magic-tokens.json")
}

func identitiesPath(claudeDir string) string {
	return filepath.Join(authDir(claudeDir), "identities.json")
}

// ---- JWT Secret ----

// LoadAuthStore loads (or creates) the JWT secret. Returns the secret bytes.
// Called from main.go at startup.
func LoadAuthStore(cfg *config.Config) []byte {
	secret, _ := loadOrCreateJWTSecret(cfg.ClaudeDir)
	return secret
}

func loadOrCreateJWTSecret(claudeDir string) ([]byte, error) {
	path := jwtSecretPath(claudeDir)
	if data, err := os.ReadFile(path); err == nil {
		b, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(b) == 32 {
			return b, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(b)), 0600); err != nil {
		return nil, err
	}
	return b, nil
}

// ---- Issued token store ----

func loadIssuedTokens(claudeDir string) []IssuedToken {
	data, err := os.ReadFile(issuedTokensPath(claudeDir))
	if err != nil {
		return nil
	}
	var tokens []IssuedToken
	_ = json.Unmarshal(data, &tokens)
	return tokens
}

func saveIssuedTokens(claudeDir string, tokens []IssuedToken) error {
	if err := os.MkdirAll(authDir(claudeDir), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(tokens, "", "  ")
	return os.WriteFile(issuedTokensPath(claudeDir), b, 0600)
}

func getIssuedTokenByJTI(claudeDir, jti string) (*IssuedToken, bool) {
	for _, t := range loadIssuedTokens(claudeDir) {
		if t.JTI == jti {
			cp := t
			return &cp, true
		}
	}
	return nil, false
}

// ---- Magic token store ----

func loadMagicTokens(claudeDir string) []magicToken {
	data, err := os.ReadFile(magicTokensPath(claudeDir))
	if err != nil {
		return nil
	}
	var tokens []magicToken
	_ = json.Unmarshal(data, &tokens)
	return tokens
}

func saveMagicTokens(claudeDir string, tokens []magicToken) error {
	if err := os.MkdirAll(authDir(claudeDir), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(tokens, "", "  ")
	return os.WriteFile(magicTokensPath(claudeDir), b, 0600)
}

// newMagicToken creates a new one-time magic token for a JTI and saves it.
// Returns the raw token string (format: "<hex>:<jti>").
func newMagicToken(claudeDir, jti string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	rawToken := hex.EncodeToString(b) + ":" + jti

	magics := loadMagicTokens(claudeDir)
	// Prune expired entries
	var valid []magicToken
	for _, m := range magics {
		if time.Now().Before(m.ExpiresAt) {
			valid = append(valid, m)
		}
	}
	valid = append(valid, magicToken{
		Token:     rawToken,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	})
	return rawToken, saveMagicTokens(claudeDir, valid)
}

// ---- Identity store ----

func loadIdentities(claudeDir string) map[string]string {
	data, err := os.ReadFile(identitiesPath(claudeDir))
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func saveIdentities(claudeDir string, m map[string]string) error {
	if err := os.MkdirAll(authDir(claudeDir), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(identitiesPath(claudeDir), b, 0600)
}

// LookupIdentity returns the JTI for a "platform:id" key if linked.
func LookupIdentity(claudeDir, platformKey string) (string, bool) {
	jti, ok := loadIdentities(claudeDir)[platformKey]
	return jti, ok
}

// ClaimsForIdentity looks up a platform:id key and returns JWT claims if a
// valid (non-revoked) token is linked. Used by the bot agent for SSO.
func ClaimsForIdentity(claudeDir, platformKey string) (*JWTClaims, bool) {
	jti, ok := LookupIdentity(claudeDir, platformKey)
	if !ok {
		return nil, false
	}
	issued, found := getIssuedTokenByJTI(claudeDir, jti)
	if !found || issued.Revoked {
		return nil, false
	}
	return &JWTClaims{
		Subject:      issued.Label,
		JTI:          issued.JTI,
		Tools:        issued.Tools,
		GoogleScopes: issued.GoogleScopes,
		IssuedAt:     issued.IssuedAt.Unix(),
		ExpiresAt:    issued.ExpiresAt.Unix(),
	}, true
}

// ---- JWT helpers ----

func issueJWT(secret []byte, issued *IssuedToken) (string, error) {
	claims := jwt.MapClaims{
		"sub":           issued.Label,
		"jti":           issued.JTI,
		"tools":         issued.Tools,
		"google_scopes": issued.GoogleScopes,
		"iat":           issued.IssuedAt.Unix(),
		"exp":           issued.ExpiresAt.Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// VerifyJWT validates a raw JWT string. Checks signature, expiry, and revocation.
// Returns nil claims with an error if validation fails.
func VerifyJWT(claudeDir string, secret []byte, rawToken string) (*JWTClaims, error) {
	tok, err := jwt.Parse(rawToken, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	jti, _ := mc["jti"].(string)
	if jti == "" {
		return nil, fmt.Errorf("missing jti claim")
	}

	issued, found := getIssuedTokenByJTI(claudeDir, jti)
	if !found || issued.Revoked {
		return nil, fmt.Errorf("token revoked or not found")
	}

	var tools []string
	if raw, ok := mc["tools"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				tools = append(tools, s)
			}
		}
	}
	var googleScopes []string
	if raw, ok := mc["google_scopes"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				googleScopes = append(googleScopes, s)
			}
		}
	}

	sub, _ := mc["sub"].(string)
	iat, _ := mc["iat"].(float64)
	exp, _ := mc["exp"].(float64)

	return &JWTClaims{
		Subject:      sub,
		JTI:          jti,
		Tools:        tools,
		GoogleScopes: googleScopes,
		IssuedAt:     int64(iat),
		ExpiresAt:    int64(exp),
	}, nil
}

// ---- HTTP handler: magic link exchange ----

// HandleMagicVerify handles GET /auth/verify?token=<magic>.
// Exchanges a one-time magic token for a signed JWT. No auth required.
func HandleMagicVerify(claudeDir string, secret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawToken := r.URL.Query().Get("token")
		if rawToken == "" {
			http.Error(w, `{"error":"missing token parameter"}`, http.StatusBadRequest)
			return
		}

		// Token format: "<hex>:<jti>"
		colonIdx := strings.Index(rawToken, ":")
		if colonIdx < 0 {
			http.Error(w, `{"error":"invalid token format"}`, http.StatusBadRequest)
			return
		}
		jti := rawToken[colonIdx+1:]

		// Find and consume the magic token
		magics := loadMagicTokens(claudeDir)
		var found bool
		var remaining []magicToken
		for _, m := range magics {
			if m.Token == rawToken && time.Now().Before(m.ExpiresAt) {
				found = true
			} else {
				remaining = append(remaining, m)
			}
		}
		if !found {
			http.Error(w, `{"error":"invalid or expired magic token"}`, http.StatusUnauthorized)
			return
		}
		// Consume: save without the used token
		_ = saveMagicTokens(claudeDir, remaining)

		issued, ok := getIssuedTokenByJTI(claudeDir, jti)
		if !ok || issued.Revoked {
			http.Error(w, `{"error":"token not found or revoked"}`, http.StatusUnauthorized)
			return
		}

		tokenStr, err := issueJWT(secret, issued)
		if err != nil {
			http.Error(w, `{"error":"failed to issue JWT"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      tokenStr,
			"jti":        jti,
			"expires_at": issued.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

// ---- MCP tool registration ----

// RegisterAuth registers the auth_* MCP tools.
func RegisterAuth(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("auth_create_token",
		mcp.WithDescription("Create a JWT token with specific tool access. Returns a magic link valid for 15 minutes that can be exchanged for a JWT."),
		mcp.WithString("label", mcp.Required(), mcp.Description("Friendly name for this token (e.g. 'vscode-alice')")),
		mcp.WithString("tools", mcp.Description("Comma-separated tool names this token can access. Empty means all tools.")),
		mcp.WithString("google_scopes", mcp.Description("Comma-separated Google scopes ('calendar' = readonly, 'calendar.full' = full access)")),
		mcp.WithNumber("expires_days", mcp.Description("Days until token expires (default 30)")),
	), authCreateTokenHandler(cfg))

	s.AddTool(mcp.NewTool("auth_list_tokens",
		mcp.WithDescription("List all non-revoked tokens with label, allowed tools, and expiry."),
	), authListTokensHandler(cfg))

	s.AddTool(mcp.NewTool("auth_revoke_token",
		mcp.WithDescription("Revoke a JWT token by its JTI. Takes effect immediately on next request."),
		mcp.WithString("jti", mcp.Required(), mcp.Description("Token JTI to revoke")),
	), authRevokeTokenHandler(cfg))

	s.AddTool(mcp.NewTool("auth_link_identity",
		mcp.WithDescription("Link a Discord, Slack, or Google identity to a JWT token for SSO. Once linked, messages from that user automatically use this token's tool ACL."),
		mcp.WithString("jti", mcp.Required(), mcp.Description("Token JTI to link the identity to")),
		mcp.WithString("platform", mcp.Required(), mcp.Description("Platform: discord, slack, or google")),
		mcp.WithString("platform_id", mcp.Required(), mcp.Description("Platform user ID or email")),
	), authLinkIdentityHandler(cfg))

	s.AddTool(mcp.NewTool("auth_list_identities",
		mcp.WithDescription("List all identity → token mappings."),
	), authListIdentitiesHandler(cfg))

	s.AddTool(mcp.NewTool("auth_unlink_identity",
		mcp.WithDescription("Remove an identity → token link."),
		mcp.WithString("platform", mcp.Required(), mcp.Description("Platform: discord, slack, or google")),
		mcp.WithString("platform_id", mcp.Required(), mcp.Description("Platform user ID or email")),
	), authUnlinkIdentityHandler(cfg))
}

func authCreateTokenHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		label, err := req.RequireString("label")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		expiresDays := req.GetInt("expires_days", 30)
		if expiresDays < 1 {
			expiresDays = 30
		}

		toolList := splitCSV(req.GetString("tools", ""))
		scopeList := expandGoogleScopes(splitCSV(req.GetString("google_scopes", "")))

		issued, magicTokenStr, err := createIssuedToken(cfg.ClaudeDir, label, toolList, scopeList, expiresDays)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create token: %v", err)), nil
		}

		baseURL := mcpBaseURL()
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Token created for %q\nJTI:     %s\nExpires: %s\n",
			label, issued.JTI, issued.ExpiresAt.Format("2006-01-02")))
		if len(toolList) > 0 {
			sb.WriteString(fmt.Sprintf("Tools:   %s\n", strings.Join(toolList, ", ")))
		} else {
			sb.WriteString("Tools:   all\n")
		}
		if len(scopeList) > 0 {
			sb.WriteString(fmt.Sprintf("Scopes:  %s\n", strings.Join(scopeList, ", ")))
		}
		sb.WriteString(fmt.Sprintf("\nMagic link (valid 15 min):\n%s/auth/verify?token=%s\n", baseURL, magicTokenStr))
		sb.WriteString(fmt.Sprintf("\ncurl \"%s/auth/verify?token=%s\"\n", baseURL, magicTokenStr))
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func authListTokensHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tokens := loadIssuedTokens(cfg.ClaudeDir)
		var sb strings.Builder
		count := 0
		for _, t := range tokens {
			if t.Revoked {
				continue
			}
			count++
			expired := ""
			if time.Now().After(t.ExpiresAt) {
				expired = " [EXPIRED]"
			}
			sb.WriteString(fmt.Sprintf("• %s%s\n  JTI: %s\n  Expires: %s\n",
				t.Label, expired, t.JTI, t.ExpiresAt.Format("2006-01-02")))
			if len(t.Tools) > 0 {
				sb.WriteString(fmt.Sprintf("  Tools: %s\n", strings.Join(t.Tools, ", ")))
			} else {
				sb.WriteString("  Tools: all\n")
			}
			if len(t.GoogleScopes) > 0 {
				sb.WriteString(fmt.Sprintf("  Google scopes: %s\n", strings.Join(t.GoogleScopes, ", ")))
			}
			sb.WriteString("\n")
		}
		if count == 0 {
			return mcp.NewToolResultText("No active tokens. Use auth_create_token to create one."), nil
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func authRevokeTokenHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jti, err := req.RequireString("jti")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		tokens := loadIssuedTokens(cfg.ClaudeDir)
		found := false
		for i, t := range tokens {
			if t.JTI == jti {
				tokens[i].Revoked = true
				found = true
				break
			}
		}
		if !found {
			return mcp.NewToolResultError(fmt.Sprintf("token %s not found", jti)), nil
		}
		if err := saveIssuedTokens(cfg.ClaudeDir, tokens); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Token %s revoked.", jti)), nil
	}
}

func authLinkIdentityHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jti, err := req.RequireString("jti")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		platform, err := req.RequireString("platform")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		platformID, err := req.RequireString("platform_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if _, found := getIssuedTokenByJTI(cfg.ClaudeDir, jti); !found {
			return mcp.NewToolResultError(fmt.Sprintf("token %s not found", jti)), nil
		}
		key := platform + ":" + platformID
		identities := loadIdentities(cfg.ClaudeDir)
		identities[key] = jti
		if err := saveIdentities(cfg.ClaudeDir, identities); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Linked %s → token %s", key, jti)), nil
	}
}

func authListIdentitiesHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		identities := loadIdentities(cfg.ClaudeDir)
		if len(identities) == 0 {
			return mcp.NewToolResultText("No identities linked. Use auth_link_identity to link one."), nil
		}
		var sb strings.Builder
		sb.WriteString("Linked identities:\n\n")
		for key, jti := range identities {
			label := "?"
			if tok, found := getIssuedTokenByJTI(cfg.ClaudeDir, jti); found {
				label = tok.Label
			}
			sb.WriteString(fmt.Sprintf("• %s → %s (%s)\n", key, jti, label))
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func authUnlinkIdentityHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		platform, err := req.RequireString("platform")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		platformID, err := req.RequireString("platform_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		key := platform + ":" + platformID
		identities := loadIdentities(cfg.ClaudeDir)
		if _, ok := identities[key]; !ok {
			return mcp.NewToolResultError(fmt.Sprintf("%s is not linked", key)), nil
		}
		delete(identities, key)
		if err := saveIdentities(cfg.ClaudeDir, identities); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Unlinked %s.", key)), nil
	}
}

// ---- CLI helper ----

// CreateTokenCLI creates a token and prints the magic link to stdout.
// Used by the auth:create CLI command in main.go.
func CreateTokenCLI(cfg *config.Config, label, toolsStr, scopesStr string, expiresDays int) error {
	if expiresDays < 1 {
		expiresDays = 30
	}
	toolList := splitCSV(toolsStr)
	scopeList := expandGoogleScopes(splitCSV(scopesStr))

	issued, magicTokenStr, err := createIssuedToken(cfg.ClaudeDir, label, toolList, scopeList, expiresDays)
	if err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}

	baseURL := mcpBaseURL()
	fmt.Printf("Token created for %q\n", label)
	fmt.Printf("JTI:     %s\n", issued.JTI)
	fmt.Printf("Expires: %s\n", issued.ExpiresAt.Format("2006-01-02"))
	if len(toolList) > 0 {
		fmt.Printf("Tools:   %s\n", strings.Join(toolList, ", "))
	} else {
		fmt.Printf("Tools:   all\n")
	}
	if len(scopeList) > 0 {
		fmt.Printf("Scopes:  %s\n", strings.Join(scopeList, ", "))
	}
	fmt.Printf("\nMagic link (valid 15 min):\n%s/auth/verify?token=%s\n", baseURL, magicTokenStr)
	return nil
}

// ---- Internal helpers ----

// createIssuedToken is the shared logic for CLI and MCP tool token creation.
func createIssuedToken(claudeDir, label string, toolList, scopeList []string, expiresDays int) (*IssuedToken, string, error) {
	jti := uuid.New().String()
	now := time.Now()
	issued := IssuedToken{
		JTI:          jti,
		Label:        label,
		Tools:        toolList,
		GoogleScopes: scopeList,
		IssuedAt:     now,
		ExpiresAt:    now.AddDate(0, 0, expiresDays),
	}
	tokens := loadIssuedTokens(claudeDir)
	tokens = append(tokens, issued)
	if err := saveIssuedTokens(claudeDir, tokens); err != nil {
		return nil, "", err
	}
	magicTokenStr, err := newMagicToken(claudeDir, jti)
	if err != nil {
		return nil, "", err
	}
	return &issued, magicTokenStr, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func expandGoogleScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, expandGoogleScope(s))
	}
	return out
}

func expandGoogleScope(s string) string {
	if strings.HasPrefix(s, "https://") {
		return s
	}
	switch s {
	case "calendar":
		return "https://www.googleapis.com/auth/calendar.readonly"
	case "calendar.full":
		return "https://www.googleapis.com/auth/calendar"
	default:
		return "https://www.googleapis.com/auth/" + s
	}
}

func mcpBaseURL() string {
	if v := os.Getenv("MCP_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

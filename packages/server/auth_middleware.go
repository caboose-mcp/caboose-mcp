package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/caboose-mcp/server/tools"
)

// authMiddleware handles all request authentication and tool ACL enforcement.
//
// Priority order:
//  1. /auth/verify path → served unauthenticated (magic link exchange)
//  2. No Authorization header → 401
//  3. Bearer matches adminToken → full admin access, no ACL check
//  4. Valid JWT → ACL check on tools/call; JWT claims injected into context
//  5. Otherwise → 401
//
// If adminToken is empty the static bypass is disabled; JWT is still supported.
func authMiddleware(adminToken string, jwtSecret []byte, claudeDir string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Magic link verification endpoint is unauthenticated by design.
		if r.URL.Path == "/auth/verify" {
			tools.HandleMagicVerify(claudeDir, jwtSecret)(w, r)
			return
		}

		bearer, ok := extractBearer(r)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Admin token bypass — full access, no ACL.
		if adminToken != "" && bearer == adminToken {
			next.ServeHTTP(w, r)
			return
		}

		// JWT path.
		claims, err := tools.VerifyJWT(claudeDir, jwtSecret, bearer)
		if err != nil {
			log.Printf("auth: JWT verification failed: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Enforce tool ACL: only applies when the token has an explicit tool list.
		if r.Method == http.MethodPost && len(claims.Tools) > 0 {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
			// Restore body so the MCP handler can read it.
			r.Body = io.NopCloser(bytes.NewReader(body))

			if toolName, ok := extractToolName(body); ok && !claimsAllowTool(claims.Tools, toolName) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK) // JSON-RPC errors always return HTTP 200
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code":    -32601,
						"message": "tool not permitted for this token: " + toolName,
					},
				})
				return
			}
		}

		// Inject claims so tool handlers can read them with tools.GetAuthClaims(ctx).
		ctx := tools.WithAuthClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractBearer(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	bearer, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || bearer == "" {
		return "", false
	}
	return bearer, true
}

// extractToolName parses the tool name from a tools/call JSON-RPC body.
// Returns ("", false) if the body is not a tools/call request.
func extractToolName(body []byte) (string, bool) {
	var rpc struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil || rpc.Method != "tools/call" {
		return "", false
	}
	return rpc.Params.Name, rpc.Params.Name != ""
}

func claimsAllowTool(allowed []string, name string) bool {
	for _, t := range allowed {
		if t == name {
			return true
		}
	}
	return false
}


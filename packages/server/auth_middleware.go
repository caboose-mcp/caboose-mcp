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

// authMiddleware handles request authentication and tool ACL enforcement.
//
// When MCP_AUTH_TOKEN is set (adminToken non-empty), auth is required:
//  1. /auth/verify path → unauthenticated (magic link exchange)
//  2. No Authorization header → 401
//  3. Bearer matches adminToken → full admin access, no ACL check
//  4. Valid JWT → ACL check on tools/call; JWT claims injected into context
//  5. Invalid token → 401
//
// When MCP_AUTH_TOKEN is not set (open/local mode), auth is optional:
//  - No bearer → request passes through without claims (tools check own credentials)
//  - Valid JWT → claims injected for per-user scoping (calendar tokens, ACL)
//  - Invalid JWT → 401 (don't silently drop a bad token)
func authMiddleware(adminToken string, jwtSecret []byte, claudeDir string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Magic link verification endpoint is always unauthenticated.
		if r.URL.Path == "/auth/verify" {
			tools.HandleMagicVerify(claudeDir, jwtSecret)(w, r)
			return
		}

		bearer, hasBearer, hasAuthHeader := extractBearer(r)

		// Admin token bypass — full access, no ACL.
		if adminToken != "" && hasBearer && bearer == adminToken {
			next.ServeHTTP(w, r)
			return
		}

		// If MCP_AUTH_TOKEN is set, an Authorization header is required.
		if adminToken != "" && !hasAuthHeader {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// No Authorization header and no admin token configured → open/local mode, pass through.
		if !hasAuthHeader {
			next.ServeHTTP(w, r)
			return
		}

		// Authorization header present but no valid non-empty Bearer token → treat as invalid token.
		if !hasBearer {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Bearer present: validate as JWT and inject claims.
		claims, err := tools.VerifyJWT(claudeDir, jwtSecret, bearer)
		if err != nil {
			log.Printf("auth: JWT verification failed: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Enforce tool ACL when the token carries an explicit allowlist.
		if r.Method == http.MethodPost && len(claims.Tools) > 0 {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
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

		ctx := tools.WithAuthClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractBearer(r *http.Request) (string, bool, bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		// No Authorization header present.
		return "", false, false
	}

	bearer, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || bearer == "" {
		// Authorization header present, but not a valid non-empty Bearer token.
		return "", false, true
	}

	// Valid non-empty Bearer token.
	return bearer, true, true
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


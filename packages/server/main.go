package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/caboose-mcp/server/tools"
	"github.com/caboose-mcp/server/tui"
	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Load .env from working directory if present (silently ignore if missing)
	_ = godotenv.Load()
	cfg := config.Load()

	if len(os.Args) < 2 {
		// Default: stdio MCP server
		s := buildMCPServer(cfg)
		if err := server.ServeStdio(s); err != nil {
			log.Fatal(err)
		}
		return
	}

	switch os.Args[1] {
	case "--setup":
		if err := tui.RunSetup(cfg); err != nil {
			log.Fatal(err)
		}

	case "--tui":
		if err := tui.Run(cfg); err != nil {
			log.Fatal(err)
		}

	case "--discord-bot":
		log.Fatal("Discord bot support is not available in this build")

	case "--slack-bot":
		if err := tools.RunSlackBot(cfg); err != nil {
			log.Fatal(err)
		}

	case "--bots":
		runBots(cfg)

	// auth:create — CLI token creation (magic link exchange)
	case "auth:create":
		fs := flag.NewFlagSet("auth:create", flag.ExitOnError)
		label := fs.String("label", "", "Friendly name for the token (required)")
		toolsFlag := fs.String("tools", "", "Comma-separated tool names (empty = all)")
		scopes := fs.String("google-scopes", "", "Comma-separated Google scopes")
		discordScopes := fs.String("discord-scopes", "", "Comma-separated Discord scopes")
		slackScopes := fs.String("slack-scopes", "", "Comma-separated Slack scopes")
		expires := fs.Int("expires", 30, "Days until token expires")
		_ = fs.Parse(os.Args[2:])
		if *label == "" {
			fmt.Fprintln(os.Stderr, "usage: fafb auth:create --label <name> [--tools ...] [--google-scopes ...] [--discord-scopes ...] [--slack-scopes ...] [--expires N]")
			os.Exit(1)
		}
		if err := tools.CreateTokenCLI(cfg, *label, *toolsFlag, *scopes, *discordScopes, *slackScopes, *expires); err != nil {
			log.Fatal(err)
		}

	case "--serve", "--serve-hosted", "--serve-local":
		addr := ":8080"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		var s *server.MCPServer
		switch os.Args[1] {
		case "--serve-hosted":
			s = buildHostedMCPServer(cfg)
		case "--serve-local":
			s = buildLocalMCPServer(cfg)
		default:
			s = buildMCPServer(cfg)
		}
		serveHTTP(cfg, addr, s)

	default:
		// Unknown flag — fall back to stdio (preserves backward compat for
		// cases where the binary is called with unexpected arguments).
		s := buildMCPServer(cfg)
		if err := server.ServeStdio(s); err != nil {
			log.Fatal(err)
		}
	}
}

// serveHTTP runs the MCP server over HTTP using the Streamable HTTP transport.
//
// Route map:
//
//	/ui/*         → embedded React UI (unauthenticated, static files)
//	/api/sandbox  → public sandbox tool execution (unauthenticated, rate-limited)
//	/auth/verify  → magic link → JWT exchange (unauthenticated, handled in authMiddleware)
//	/*            → authMiddleware → MCP server (bearer token or JWT required)
func serveHTTP(cfg *config.Config, addr string, s *server.MCPServer) {
	jwtSecret := tools.LoadAuthStore(cfg)

	httpSrv := server.NewStreamableHTTPServer(s,
		server.WithEndpointPath("/mcp"),
		server.WithStateLess(true),
	)

	adminToken := os.Getenv("MCP_AUTH_TOKEN")
	authedMCP := authMiddleware(adminToken, jwtSecret, cfg.ClaudeDir, httpSrv)

	mux := http.NewServeMux()
	// /ui/* → 301 redirect to the standalone UI repo (path-preserving)
	mux.HandleFunc("/ui/", func(w http.ResponseWriter, r *http.Request) {
		target := cfg.UIOrigin + strings.TrimPrefix(r.URL.Path, "/ui")
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		target := cfg.UIOrigin + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
	// Public routes (no auth)
	mux.HandleFunc("/api/sandbox", sandboxHandler(cfg))
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"env":%q}`, cfg.ReleaseStage)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		total := len(s.ListTools())
		fmt.Fprintf(w, `{"total":%d}`, total)
	})
	// Discord OAuth routes (unauthenticated)
	mux.HandleFunc("/auth/discord/start", discordOAuthStart(cfg))
	mux.HandleFunc("/auth/discord/callback", discordOAuthCallback(cfg, jwtSecret))
	// Authenticated routes
	mux.Handle("/", authedMCP)

	if adminToken != "" {
		log.Printf("fafb HTTP server on %s (admin token + JWT auth)", addr)
	} else {
		log.Printf("fafb HTTP server on %s (JWT auth only — set MCP_AUTH_TOKEN for admin bypass)", addr)
	}
	log.Printf("UI:      %s", cfg.UIOrigin)
	log.Printf("MCP:     http://%s/mcp", addr)
	log.Printf("Sandbox: http://%s/api/sandbox", addr)
	log.Printf("Health:  http://%s/health", addr)
	log.Printf("Release: %s", cfg.ReleaseStage)

	if err := http.ListenAndServe(addr, corsMiddleware(cfg.UIOrigin, mux)); err != nil {
		log.Fatal(err)
	}
}

// registerCommonTools registers tools available in both hosted and local modes.
// These tools have no dependency on local hardware and are safe to run everywhere.
func registerCommonTools(s *server.MCPServer, cfg *config.Config) {
	tools.RegisterJokes(s, cfg)
}

// registerHostedTools registers tools that are safe to run in a cloud/hosted environment.
// These tools have no dependency on local hardware, Docker, or LAN-connected devices.
func registerHostedTools(s *server.MCPServer, cfg *config.Config) {
	tools.RegisterClaude(s, cfg)
	tools.RegisterSecrets(s, cfg)
	tools.RegisterGitHub(s, cfg)
	tools.RegisterDatabase(s, cfg)
	tools.RegisterSlack(s, cfg)
	tools.RegisterDiscord(s, cfg)
	tools.RegisterEnv(s, cfg)
	tools.RegisterMermaid(s, cfg)
	tools.RegisterGreptile(s, cfg)
	tools.RegisterSelfImprove(s, cfg)
	tools.RegisterSetup(s, cfg)
	tools.RegisterCloudSync(s, cfg)
	tools.RegisterHealth(s, cfg)
	tools.RegisterPersona(s, cfg)
	tools.RegisterSandbox(s, cfg)
	tools.RegisterAudit(s, cfg)
	tools.RegisterAuth(s, cfg)
	tools.RegisterSources(s, cfg)
	tools.RegisterRepo(s, cfg) // Phase 3: Repository management tools (repo create, test, approve, deploy)
	tools.RegisterGamma(s, cfg) // Phase 4: Gamma presentation generation
	tools.RegisterOrgHealth(s, cfg)   // org_health_status, org_health_refresh, org_health_next_pr
	tools.RegisterOrgManager(s, cfg)  // org_list_repos, org_sync_status, org_pr_dashboard, org_pull_all, org_branch_cleanup
	registerCommonTools(s, cfg)
}

// registerLocalTools registers tools that require local hardware or LAN access.
// These tools depend on Docker, a local shell, Bambu printer, Blender, Chezmoi, or local source code.
func registerLocalTools(s *server.MCPServer, cfg *config.Config) {
	tools.RegisterDocker(s, cfg)
	tools.RegisterSystem(s, cfg)
	tools.RegisterPrinting(s, cfg)
	tools.RegisterChezmoi(s, cfg)
	tools.RegisterToolsmith(s, cfg)
	tools.RegisterAgency(s, cfg)
	registerCommonTools(s, cfg)
}

// mcpServerOptions returns the base options for all MCP server builds,
// including an experimental disclaimer when ReleaseStage != "stable".
func mcpServerOptions(cfg *config.Config) []server.ServerOption {
	opts := []server.ServerOption{server.WithToolCapabilities(false)}
	if cfg.ReleaseStage != "stable" {
		opts = append(opts, server.WithInstructions(
			"⚠️  EXPERIMENTAL SOFTWARE — USE AT YOUR OWN RISK\n\n"+
				"fafb is under active development and has not been fully tested. "+
				"Tools may behave unexpectedly, produce incorrect results, modify data, or fail without warning. "+
				"No warranty is provided, express or implied. "+
				"Do not use this server for critical workflows, production systems, or sensitive data without understanding the risks.\n\n"+
				"Set FAFB_ENV=stable to suppress this message once the deployment is considered production-ready.",
		))
	}
	return opts
}

// buildHostedMCPServer creates a server with only cloud-safe hosted tools.
// Used for --serve-hosted and ECS Fargate deployments.
func buildHostedMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("fafb", "2.0.0", mcpServerOptions(cfg)...)
	registerHostedTools(s, cfg)
	return s
}

// buildLocalMCPServer creates a server with only local/hardware tools.
// Used for --serve-local when running on the Pi alongside Claude Code.
func buildLocalMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("fafb", "2.0.0", mcpServerOptions(cfg)...)
	registerLocalTools(s, cfg)
	return s
}

// buildMCPServer creates a server with all tools (hosted + local combined).
// Used for --serve and stdio (default) modes.
func buildMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("fafb", "2.0.0", mcpServerOptions(cfg)...)
	registerHostedTools(s, cfg)
	registerLocalTools(s, cfg)
	return s
}

// runBots starts the Slack and Discord bots concurrently. If either exits with
// an error it is logged but the other bot continues running. The process blocks
// until both have exited.
func runBots(cfg *config.Config) {
	ctx := context.Background()
	type botFn struct {
		name string
		run  func() error
	}
	bots := []botFn{
		{"slack", func() error { return tools.RunSlackBot(cfg) }},
		{"discord", func() error { return tools.RunDiscordBot(ctx, cfg) }},
	}

	var wg sync.WaitGroup
	for _, b := range bots {
		b := b
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("starting %s bot", b.name)
			if err := b.run(); err != nil {
				log.Printf("%s bot exited with error: %v", b.name, err)
			} else {
				log.Printf("%s bot exited", b.name)
			}
		}()
	}
	wg.Wait()
}

// ---- Discord OAuth Handlers ----

// discordOAuthStart generates a Discord OAuth consent URL and redirects the user.
// Query parameters:
//   - redirect_uri (optional): where to send the user after auth (default: cfg.UIOrigin)
func discordOAuthStart(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.DiscordOAuthClientID == "" {
			http.Error(w, "Discord OAuth not configured", http.StatusInternalServerError)
			return
		}

		// Generate CSRF state token (opaque 16-byte random string)
		stateBytes := make([]byte, 16)
		if _, err := rand.Read(stateBytes); err != nil {
			http.Error(w, "Failed to generate state token", http.StatusInternalServerError)
			return
		}
		state := hex.EncodeToString(stateBytes)

		// Store state in session cookie (expires in 10 minutes)
		http.SetCookie(w, &http.Cookie{
			Name:     "discord_oauth_state",
			Value:    state,
			Path:     "/",
			MaxAge:   600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		// Get redirect_uri from query params or use UI origin
		redirectURI := r.URL.Query().Get("redirect_uri")
		if redirectURI == "" {
			redirectURI = cfg.DiscordOAuthRedirectURI
		}

		// Validate redirect_uri against allowed origins (only UI origin allowed)
		if !isAllowedRedirectURI(redirectURI, cfg.UIOrigin) {
			http.Error(w, "Invalid redirect_uri", http.StatusBadRequest)
			return
		}

		// Build Discord auth URL
		authURL := fmt.Sprintf(
			"https://discord.com/api/oauth2/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=identify%%20email&state=%s",
			url.QueryEscape(cfg.DiscordOAuthClientID),
			url.QueryEscape(redirectURI),
			url.QueryEscape(state),
		)

		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// discordOAuthCallback handles the Discord OAuth callback.
// Query parameters:
//   - code: authorization code from Discord
//   - state: CSRF token (must match cookie)
//   - error: if authorization was denied
func discordOAuthCallback(cfg *config.Config, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for OAuth errors
		if err := r.URL.Query().Get("error"); err != "" {
			errDesc := r.URL.Query().Get("error_description")
			http.Error(w, fmt.Sprintf("Authorization denied: %s", errDesc), http.StatusUnauthorized)
			return
		}

		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if code == "" || state == "" {
			http.Error(w, "Missing code or state parameter", http.StatusBadRequest)
			return
		}

		// Verify CSRF state
		cookie, err := r.Cookie("discord_oauth_state")
		if err != nil || cookie.Value != state {
			http.Error(w, "Invalid state token (CSRF check failed)", http.StatusUnauthorized)
			return
		}

		// Exchange code for token
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		err = tools.GetDiscordOAuthProvider().ExchangeCode(ctx, cfg, code)
		if err != nil {
			log.Printf("Discord OAuth code exchange failed: %v", err)
			http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
			return
		}

		// Fetch Discord user info
		user, err := tools.GetDiscordUser(ctx, cfg)
		if err != nil {
			log.Printf("Failed to fetch Discord user info: %v", err)
			http.Error(w, "Failed to fetch user info", http.StatusInternalServerError)
			return
		}

		// Create or link JWT token for this Discord user
		token, err := tools.LinkDiscordIdentity(cfg, jwtSecret, user.ID, user.Username)
		if err != nil {
			log.Printf("Failed to link Discord identity: %v", err)
			http.Error(w, "Failed to create token", http.StatusInternalServerError)
			return
		}

		// Calculate expiry time
		expiresAt := token.ExpiresAt.Format("2006-01-02T15:04:05Z")

		// Redirect to UI callback with token and metadata
		callbackURL := fmt.Sprintf(
			"%s/auth/callback?token=%s&expires_at=%s&username=%s&discord_id=%s",
			cfg.UIOrigin,
			url.QueryEscape(token.JWT),
			url.QueryEscape(expiresAt),
			url.QueryEscape(user.Username),
			url.QueryEscape(user.ID),
		)

		http.Redirect(w, r, callbackURL, http.StatusFound)
	}
}

// isAllowedRedirectURI validates that the redirect_uri is safe (i.e., points to the UI origin).
func isAllowedRedirectURI(redirectURI, uiOrigin string) bool {
	if redirectURI == "" {
		return false
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		return false
	}
	uiURL, err := url.Parse(uiOrigin)
	if err != nil {
		return false
	}
	return u.Scheme == uiURL.Scheme && u.Host == uiURL.Host
}

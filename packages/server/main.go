package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

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
			fmt.Fprintln(os.Stderr, "usage: caboose-mcp auth:create --label <name> [--tools ...] [--google-scopes ...] [--discord-scopes ...] [--slack-scopes ...] [--expires N]")
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
	// Authenticated routes
	mux.Handle("/", authedMCP)

	if adminToken != "" {
		log.Printf("caboose-mcp HTTP server on %s (admin token + JWT auth)", addr)
	} else {
		log.Printf("caboose-mcp HTTP server on %s (JWT auth only — set MCP_AUTH_TOKEN for admin bypass)", addr)
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
				"caboose-mcp is under active development and has not been fully tested. "+
				"Tools may behave unexpectedly, produce incorrect results, modify data, or fail without warning. "+
				"No warranty is provided, express or implied. "+
				"Do not use this server for critical workflows, production systems, or sensitive data without understanding the risks.\n\n"+
				"Set CABOOSE_ENV=stable to suppress this message once the deployment is considered production-ready.",
		))
	}
	return opts
}

// buildHostedMCPServer creates a server with only cloud-safe hosted tools.
// Used for --serve-hosted and ECS Fargate deployments.
func buildHostedMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("caboose-mcp", "2.0.0", mcpServerOptions(cfg)...)
	registerHostedTools(s, cfg)
	return s
}

// buildLocalMCPServer creates a server with only local/hardware tools.
// Used for --serve-local when running on the Pi alongside Claude Code.
func buildLocalMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("caboose-mcp", "2.0.0", mcpServerOptions(cfg)...)
	registerLocalTools(s, cfg)
	return s
}

// buildMCPServer creates a server with all tools (hosted + local combined).
// Used for --serve and stdio (default) modes.
func buildMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("caboose-mcp", "2.0.0", mcpServerOptions(cfg)...)
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

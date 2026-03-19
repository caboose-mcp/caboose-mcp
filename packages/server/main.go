package main

import (
	"context"
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

	// --setup flag: interactive config wizard, writes .env file
	if len(os.Args) > 1 && os.Args[1] == "--setup" {
		if err := tui.RunSetup(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	// --tui flag: launch terminal UI instead of MCP server
	if len(os.Args) > 1 && os.Args[1] == "--tui" {
		if err := tui.Run(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	// --discord-bot: Discord gateway bot is not available in this build.
	if len(os.Args) > 1 && os.Args[1] == "--discord-bot" {
		log.Fatal("Discord bot support is not available in this build; the Discord gateway bot implementation has not been added yet")
		return
	}

	// --slack-bot: run the Slack Socket Mode bot (blocks; run as a service).

	// --slack-bot: run the Slack Socket Mode bot (blocks; run as a service).
	if len(os.Args) > 1 && os.Args[1] == "--slack-bot" {
		if err := tools.RunSlackBot(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	// --bots: run both Slack and Discord bots concurrently.
	// If one dies it logs the error and continues; the other keeps running.
	if len(os.Args) > 1 && os.Args[1] == "--bots" {
		runBots(cfg)
		return
	}

	// --serve [addr]: HTTP server with all tools (hosted + local).
	// --serve-hosted [addr]: HTTP server with cloud-safe tools only (no Docker/Bambu/Blender/shell).
	// --serve-local [addr]: HTTP server with local-only tools (Docker, shell, Bambu, Blender, Chezmoi, Toolsmith).
	// addr defaults to :8080. Set MCP_AUTH_TOKEN to require bearer auth.
	for _, flag := range []string{"--serve", "--serve-hosted", "--serve-local"} {
		if len(os.Args) > 1 && os.Args[1] == flag {
			addr := ":8080"
			if len(os.Args) > 2 {
				addr = os.Args[2]
			}
			var s *server.MCPServer
			switch flag {
			case "--serve-hosted":
				s = buildHostedMCPServer(cfg)
			case "--serve-local":
				s = buildLocalMCPServer(cfg)
			default:
				s = buildMCPServer(cfg)
			}
			serveHTTP(cfg, addr, s)
			return
		}
	}

	s := buildMCPServer(cfg)
	if err := server.ServeStdio(s); err != nil {
		log.Fatal(err)
	}
}

// serveHTTP runs the MCP server over HTTP using the Streamable HTTP transport.
// If MCP_AUTH_TOKEN is set, all requests must include "Authorization: Bearer <token>".
func serveHTTP(cfg *config.Config, addr string, s *server.MCPServer) {
	httpSrv := server.NewStreamableHTTPServer(s,
		server.WithEndpointPath("/mcp"),
		server.WithStateLess(true),
	)

	authToken := os.Getenv("MCP_AUTH_TOKEN")
	var handler http.Handler = httpSrv
	if authToken != "" {
		handler = bearerAuthMiddleware(authToken, httpSrv)
		log.Printf("caboose-mcp HTTP server on %s (bearer auth enabled)", addr)
	} else {
		log.Printf("caboose-mcp HTTP server on %s (WARNING: no MCP_AUTH_TOKEN set — server is unauthenticated)", addr)
	}

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
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
	tools.RegisterLearning(s, cfg)
	tools.RegisterSetup(s, cfg)
	tools.RegisterCloudSync(s, cfg)
	tools.RegisterHealth(s, cfg)
	tools.RegisterPersona(s, cfg)
	tools.RegisterFocus(s, cfg)
	tools.RegisterJokes(s, cfg)
	tools.RegisterCalendar(s, cfg)
	tools.RegisterNotes(s, cfg)
	tools.RegisterSources(s, cfg)
	tools.RegisterSandbox(s, cfg)
	tools.RegisterAudit(s, cfg)
}

// registerLocalTools registers tools that require local hardware or LAN access.
// These tools depend on Docker, a local shell, Bambu printer, Blender, Chezmoi, or local source code.
func registerLocalTools(s *server.MCPServer, cfg *config.Config) {
	tools.RegisterDocker(s, cfg)
	tools.RegisterSystem(s, cfg)
	tools.RegisterPrinting(s, cfg)
	tools.RegisterChezmoi(s, cfg)
	tools.RegisterToolsmith(s, cfg)
}

// buildHostedMCPServer creates a server with only cloud-safe hosted tools.
// Used for --serve-hosted and ECS Fargate deployments.
func buildHostedMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("caboose-mcp", "2.0.0",
		server.WithToolCapabilities(false),
	)
	registerHostedTools(s, cfg)
	return s
}

// buildLocalMCPServer creates a server with only local/hardware tools.
// Used for --serve-local when running on the Pi alongside Claude Code.
func buildLocalMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("caboose-mcp", "2.0.0",
		server.WithToolCapabilities(false),
	)
	registerLocalTools(s, cfg)
	return s
}

// buildMCPServer creates a server with all tools (hosted + local combined).
// Used for --serve and stdio (default) modes.
func buildMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("caboose-mcp", "2.0.0",
		server.WithToolCapabilities(false),
	)
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

// bearerAuthMiddleware rejects requests without the correct Authorization header.
func bearerAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

package main

import (
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

	// --discord-bot: run the Discord gateway bot (blocks; run as a service).
	if len(os.Args) > 1 && os.Args[1] == "--discord-bot" {
		if err := tools.RunDiscordBot(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

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

	// --serve [addr]: run HTTP/SSE transport instead of stdio.
	// addr defaults to :8080. Set MCP_AUTH_TOKEN to require bearer auth.
	// Example: ./caboose-mcp --serve :9090
	if len(os.Args) > 1 && os.Args[1] == "--serve" {
		addr := ":8080"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		serveHTTP(cfg, addr)
		return
	}

	s := buildMCPServer(cfg)
	if err := server.ServeStdio(s); err != nil {
		log.Fatal(err)
	}
}

// serveHTTP runs the MCP server over HTTP using the Streamable HTTP transport.
// If MCP_AUTH_TOKEN is set, all requests must include "Authorization: Bearer <token>".
func serveHTTP(cfg *config.Config, addr string) {
	s := buildMCPServer(cfg)
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

// buildMCPServer creates and fully registers the MCP server.
// Extracted so both stdio and HTTP paths share the same tool set.
func buildMCPServer(cfg *config.Config) *server.MCPServer {
	s := server.NewMCPServer("caboose-mcp", "2.0.0",
		server.WithToolCapabilities(false),
	)
	tools.RegisterClaude(s, cfg)
	tools.RegisterSecrets(s, cfg)
	tools.RegisterGitHub(s, cfg)
	tools.RegisterDocker(s, cfg)
	tools.RegisterDatabase(s, cfg)
	tools.RegisterSystem(s, cfg)
	tools.RegisterSlack(s, cfg)
	tools.RegisterDiscord(s, cfg)
	tools.RegisterPrinting(s, cfg)
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
	tools.RegisterChezmoi(s, cfg)
	tools.RegisterToolsmith(s, cfg)
	tools.RegisterSandbox(s, cfg)
	tools.RegisterAudit(s, cfg)
	return s
}

// runBots starts the Slack and Discord bots concurrently. If either exits with
// an error it is logged but the other bot continues running. The process blocks
// until both have exited.
func runBots(cfg *config.Config) {
	bots := []struct {
		name string
		fn   func(*config.Config) error
	}{
		{"slack", tools.RunSlackBot},
		{"discord", tools.RunDiscordBot},
	}

	var wg sync.WaitGroup
	for _, b := range bots {
		b := b
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("starting %s bot", b.name)
			if err := b.fn(cfg); err != nil {
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

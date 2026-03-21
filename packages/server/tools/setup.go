package tools

// setup_check — interactively verify and report configuration status.
// Reports which features are enabled/disabled and what's needed to enable them.
// No external calls; pure local inspection.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterSetup(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("setup_bot_configure",
		mcp.WithDescription("Guided interactive setup for Discord or Slack bot integration. Generates .env configuration templates and setup instructions."),
		mcp.WithString("platform", mcp.Required(), mcp.Description("Platform to configure: 'discord', 'slack', or 'both'")),
	), setupBotConfigureHandler(cfg))
	s.AddTool(mcp.NewTool("setup_github_mcp_info",
		mcp.WithDescription("Explains how to use GitHub's official MCP server alongside caboose-mcp, and when each approach is better."),
	), setupGitHubMCPInfoHandler(cfg))
	s.AddTool(mcp.NewTool("setup_check",
		mcp.WithDescription("Check the current caboose-mcp configuration and report which features are enabled, disabled, or misconfigured."),
	), setupCheckHandler(cfg))

	s.AddTool(mcp.NewTool("setup_write_env",
		mcp.WithDescription("Write a .env file with the provided key=value pairs to a given path."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to write (e.g. /home/user/dev/caboose-mcp/.env)")),
		mcp.WithString("vars", mcp.Required(), mcp.Description("Newline-separated KEY=VALUE pairs")),
	), setupWriteEnvHandler(cfg))

	s.AddTool(mcp.NewTool("setup_init_dirs",
		mcp.WithDescription("Initialize required CLAUDE_DIR subdirectories (secrets/, pending/, errors/, learning/)."),
	), setupInitDirsHandler(cfg))

	s.AddTool(mcp.NewTool("setup_n8n_workflows",
		mcp.WithDescription("Return example n8n workflow JSON for scheduling recurring caboose-mcp tasks (tech digest, learning sessions, self-scan)."),
		mcp.WithString("binary_path", mcp.Description("Path to caboose-mcp binary (default: /home/caboose/dev/caboose-mcp/caboose-mcp)")),
	), setupN8nWorkflowsHandler(cfg))
}

type checkItem struct {
	feature string
	status  string // OK | MISSING | WARN
	detail  string
}

func setupCheckHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var checks []checkItem

		// CLAUDE_DIR
		if _, err := os.Stat(cfg.ClaudeDir); err == nil {
			checks = append(checks, checkItem{"CLAUDE_DIR", "OK", cfg.ClaudeDir})
		} else {
			checks = append(checks, checkItem{"CLAUDE_DIR", "WARN", fmt.Sprintf("%s does not exist yet (will be created on first use)", cfg.ClaudeDir)})
		}

		// GPG
		if cfg.GPGKeyID != "" {
			if _, err := exec.LookPath("gpg"); err != nil {
				checks = append(checks, checkItem{"GPG secrets", "WARN", fmt.Sprintf("GPG_KEY_ID set (%s) but gpg not found in PATH", cfg.GPGKeyID)})
			} else {
				checks = append(checks, checkItem{"GPG secrets", "OK", fmt.Sprintf("key: %s", cfg.GPGKeyID)})
			}
		} else {
			checks = append(checks, checkItem{"GPG secrets", "MISSING", "Set GPG_KEY_ID to your GPG key ID (gpg --list-keys)"})
		}

		// Slack
		if cfg.SlackToken != "" {
			checks = append(checks, checkItem{"Slack", "OK", "SLACK_TOKEN set"})
		} else {
			checks = append(checks, checkItem{"Slack", "MISSING", "Set SLACK_TOKEN to a Bot OAuth token (xoxb-...)"})
		}

		// Discord
		if cfg.DiscordToken != "" {
			checks = append(checks, checkItem{"Discord", "OK", "DISCORD_TOKEN set"})
		} else {
			checks = append(checks, checkItem{"Discord", "MISSING", "Set DISCORD_TOKEN to your bot token"})
		}

		// ElevenLabs TTS
		if cfg.ElevenLabsAPIKey != "" && cfg.ElevenLabsVoiceID != "" {
			checks = append(checks, checkItem{"ElevenLabs TTS", "OK", fmt.Sprintf("voice=%s", cfg.ElevenLabsVoiceID)})
		} else if cfg.ElevenLabsAPIKey != "" {
			checks = append(checks, checkItem{"ElevenLabs TTS", "WARN", "ELEVENLABS_API_KEY set but ELEVENLABS_VOICE_ID is missing — TTS disabled until both are set"})
		} else {
			checks = append(checks, checkItem{"ElevenLabs TTS", "MISSING", "Set ELEVENLABS_API_KEY and ELEVENLABS_VOICE_ID to enable voice replies in Discord/Slack"})
		}

		// Bambu
		if cfg.BambuIP != "" && cfg.BambuSerial != "" && cfg.BambuAccessCode != "" {
			checks = append(checks, checkItem{"Bambu A1", "OK", fmt.Sprintf("IP=%s serial=%s", cfg.BambuIP, cfg.BambuSerial)})
		} else {
			missing := []string{}
			if cfg.BambuIP == "" {
				missing = append(missing, "BAMBU_IP")
			}
			if cfg.BambuSerial == "" {
				missing = append(missing, "BAMBU_SERIAL")
			}
			if cfg.BambuAccessCode == "" {
				missing = append(missing, "BAMBU_ACCESS_CODE")
			}
			checks = append(checks, checkItem{"Bambu A1", "MISSING", "Set: " + strings.Join(missing, ", ")})
		}

		// Greptile
		if cfg.GreptileAPIKey != "" {
			checks = append(checks, checkItem{"Greptile", "OK", fmt.Sprintf("key set, repo=%s", cfg.GreptileRepo)})
		} else {
			checks = append(checks, checkItem{"Greptile", "MISSING", "Set GREPTILE_API_KEY"})
		}

		// Postgres
		if cfg.PostgresURL != "" {
			checks = append(checks, checkItem{"PostgreSQL", "OK", "POSTGRES_URL set"})
		} else {
			checks = append(checks, checkItem{"PostgreSQL", "MISSING", "Set POSTGRES_URL (or pass connection_string per-call)"})
		}

		// MongoDB
		if cfg.MongoURL != "" {
			checks = append(checks, checkItem{"MongoDB", "OK", "MONGO_URL set"})
		} else {
			checks = append(checks, checkItem{"MongoDB", "MISSING", "Set MONGO_URL (or pass connection_string per-call)"})
		}

		// CLI tools
		cliTools := []string{"gh", "docker", "git", "gpg", "psql", "blender"}
		for _, tool := range cliTools {
			if _, err := exec.LookPath(tool); err == nil {
				checks = append(checks, checkItem{"CLI: " + tool, "OK", ""})
			} else {
				checks = append(checks, checkItem{"CLI: " + tool, "WARN", "not found in PATH (required for some tools)"})
			}
		}

		// Format output
		var lines []string
		lines = append(lines, "=== caboose-mcp Setup Check ===\n")
		ok, warn, missing := 0, 0, 0
		for _, c := range checks {
			icon := "✓"
			if c.status == "MISSING" {
				icon = "✗"
				missing++
			} else if c.status == "WARN" {
				icon = "!"
				warn++
			} else {
				ok++
			}
			detail := ""
			if c.detail != "" {
				detail = " — " + c.detail
			}
			lines = append(lines, fmt.Sprintf("  [%s] %s%s", icon, c.feature, detail))
		}
		lines = append(lines, fmt.Sprintf("\nSummary: %d OK, %d warnings, %d missing", ok, warn, missing))
		if missing > 0 {
			lines = append(lines, "See .env.example for configuration reference.")
		}

		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}
}

func setupWriteEnvHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		vars, err := req.RequireString("vars")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("mkdir: %v", err)), nil
		}
		if err := os.WriteFile(path, []byte(vars+"\n"), 0600); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("wrote %s (mode 0600)", path)), nil
	}
}

func setupInitDirsHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dirs := []string{
			filepath.Join(cfg.ClaudeDir, "secrets"),
			filepath.Join(cfg.ClaudeDir, "pending"),
			filepath.Join(cfg.ClaudeDir, "errors"),
			filepath.Join(cfg.ClaudeDir, "learning"),
		}
		var created, existed []string
		for _, d := range dirs {
			if _, err := os.Stat(d); err == nil {
				existed = append(existed, d)
				continue
			}
			if err := os.MkdirAll(d, 0755); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("mkdir %s: %v", d, err)), nil
			}
			created = append(created, d)
		}
		var msg []string
		if len(created) > 0 {
			msg = append(msg, "Created:\n  "+strings.Join(created, "\n  "))
		}
		if len(existed) > 0 {
			msg = append(msg, "Already existed:\n  "+strings.Join(existed, "\n  "))
		}
		return mcp.NewToolResultText(strings.Join(msg, "\n")), nil
	}
}

func setupN8nWorkflowsHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Returns three importable n8n v1.x workflow JSON objects.
		// Import via: n8n UI → Settings → Import Workflow (paste each JSON separately).
		//
		// Set CABOOSE_BIN as an n8n environment variable (Settings → Environment Variables)
		// or as a system env var visible to the n8n worker process.

		result := map[string]any{
			"event_receiver": buildEventReceiverWorkflow(),
			"daily_digest":   buildDailyDigestWorkflow(),
			"nightly_scan":   buildNightlyScanWorkflow(),
		}

		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
		}

		header := "=== n8n Importable Workflows ===\n" +
			"Import each workflow individually: n8n → Settings → Import Workflow.\n" +
			"Set CABOOSE_BIN env var to the caboose-mcp binary path in n8n settings.\n\n"

		return mcp.NewToolResultText(header + string(b)), nil
	}
}

// n8n node/connection helpers

func n8nNode(id, name, nodeType string, typeVersion int, pos [2]int, params map[string]any) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        name,
		"type":        nodeType,
		"typeVersion": typeVersion,
		"position":    pos,
		"parameters":  params,
	}
}

func n8nConn(targetNode string) map[string]any {
	return map[string]any{"node": targetNode, "type": "main", "index": 0}
}

// mcpCmd builds the shell command to call a caboose-mcp tool via stdio.
// Uses $CABOOSE_BIN with a fallback to the default binary path.
func mcpCmd(toolName string, args map[string]any) string {
	argsJSON, _ := json.Marshal(args)
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`,
		toolName, string(argsJSON))
	return fmt.Sprintf(
		`CABOOSE_BIN="${CABOOSE_BIN:-/home/caboose/dev/caboose-mcp/caboose-mcp}"; echo '%s' | "$CABOOSE_BIN"`,
		payload,
	)
}

// buildEventReceiverWorkflow returns Workflow A.
// Receives push events from EmitEvent (set N8N_WEBHOOK_URL to this webhook's URL).
// Switch routes on event type; Set nodes format a human-readable message.
// Connect the Set nodes to Slack/Discord/email nodes as desired.
func buildEventReceiverWorkflow() map[string]any {
	nodes := []any{
		n8nNode("wha-001", "Caboose Events", "n8n-nodes-base.webhook", 2, [2]int{250, 300}, map[string]any{
			"httpMethod":   "POST",
			"path":         "caboose-events",
			"responseMode": "onReceived",
		}),
		n8nNode("wha-002", "Route by Event Type", "n8n-nodes-base.switch", 1, [2]int{500, 300}, map[string]any{
			"dataType": "string",
			"value1":   "={{ $json.type }}",
			"rules": map[string]any{
				"rules": []any{
					map[string]any{"value2": "gate_fired", "output": 0},
					map[string]any{"value2": "source_changed", "output": 1},
					map[string]any{"value2": "suggestion_created", "output": 2},
					map[string]any{"value2": "error_reported", "output": 3},
					map[string]any{"value2": "focus_started", "output": 4},
					map[string]any{"value2": "focus_ended", "output": 5},
				},
			},
			"fallbackOutput": "none",
		}),
		n8nNode("wha-003", "Gate Alert", "n8n-nodes-base.set", 2, [2]int{800, 60}, map[string]any{
			"values": map[string]any{
				"string": []any{map[string]any{
					"name":  "message",
					"value": `=` + "`" + `Gate pending: ${$json.data.tool} — ID: ${$json.data.gate_id}\nApprove: ${$json.data.approve_cmd}\nDeny: ${$json.data.deny_cmd}` + "`",
				}},
			},
		}),
		n8nNode("wha-004", "Source Update", "n8n-nodes-base.set", 2, [2]int{800, 180}, map[string]any{
			"values": map[string]any{
				"string": []any{map[string]any{
					"name":  "message",
					"value": `=` + "`" + `Source updated: ${$json.data.source_name} (${$json.data.source_type})\n${$json.data.summary}` + "`",
				}},
			},
		}),
		n8nNode("wha-005", "Suggestion Notification", "n8n-nodes-base.set", 2, [2]int{800, 300}, map[string]any{
			"values": map[string]any{
				"string": []any{map[string]any{
					"name":  "message",
					"value": `=` + "`" + `New suggestion [${$json.data.status}]: ${$json.data.title}\nCategory: ${$json.data.category} | Dir: ${$json.data.dir}` + "`",
				}},
			},
		}),
		n8nNode("wha-006", "Error Alert", "n8n-nodes-base.set", 2, [2]int{800, 420}, map[string]any{
			"values": map[string]any{
				"string": []any{map[string]any{
					"name":  "message",
					"value": `=` + "`" + `Error from ${$json.data.source}: ${$json.data.message}` + "`",
				}},
			},
		}),
		n8nNode("wha-007", "Focus Started", "n8n-nodes-base.set", 2, [2]int{800, 540}, map[string]any{
			"values": map[string]any{
				"string": []any{map[string]any{
					"name":  "message",
					"value": `=` + "`" + `Focus started: ${$json.data.goal}` + "`",
				}},
			},
		}),
		n8nNode("wha-008", "Focus Ended", "n8n-nodes-base.set", 2, [2]int{800, 660}, map[string]any{
			"values": map[string]any{
				"string": []any{map[string]any{
					"name":  "message",
					"value": `=` + "`" + `Focus ended: ${$json.data.goal} | Parked: ${$json.data.parked_count}` + "`",
				}},
			},
		}),
	}

	connections := map[string]any{
		"Caboose Events": map[string]any{
			"main": []any{[]any{n8nConn("Route by Event Type")}},
		},
		"Route by Event Type": map[string]any{
			"main": []any{
				[]any{n8nConn("Gate Alert")},
				[]any{n8nConn("Source Update")},
				[]any{n8nConn("Suggestion Notification")},
				[]any{n8nConn("Error Alert")},
				[]any{n8nConn("Focus Started")},
				[]any{n8nConn("Focus Ended")},
			},
		},
	}

	return map[string]any{
		"name":        "Caboose Event Receiver",
		"nodes":       nodes,
		"connections": connections,
		"active":      false,
		"settings":    map[string]any{"executionOrder": "v1"},
		"id":          "caboose-event-receiver-v1",
	}
}

// buildDailyDigestWorkflow returns Workflow B — runs at 8am daily.
func buildDailyDigestWorkflow() map[string]any {
	nodes := []any{
		n8nNode("whb-001", "8am Daily", "n8n-nodes-base.scheduleTrigger", 1, [2]int{250, 300}, map[string]any{
			"rule": map[string]any{
				"interval": []any{map[string]any{
					"field": "cronExpression", "expression": "0 8 * * *",
				}},
			},
		}),
		n8nNode("whb-002", "Source Digest", "n8n-nodes-base.executeCommand", 1, [2]int{500, 220}, map[string]any{
			"command": mcpCmd("source_digest", map[string]any{}),
		}),
		n8nNode("whb-003", "Tech Digest", "n8n-nodes-base.executeCommand", 1, [2]int{500, 380}, map[string]any{
			"command": mcpCmd("si_tech_digest", map[string]any{"dir": "/home/caboose/dev"}),
		}),
	}

	connections := map[string]any{
		"8am Daily": map[string]any{
			"main": []any{[]any{n8nConn("Source Digest"), n8nConn("Tech Digest")}},
		},
	}

	return map[string]any{
		"name":        "Caboose Daily Digest",
		"nodes":       nodes,
		"connections": connections,
		"active":      false,
		"settings":    map[string]any{"executionOrder": "v1"},
		"id":          "caboose-daily-digest-v1",
	}
}

// buildNightlyScanWorkflow returns Workflow C — runs at midnight.
func buildNightlyScanWorkflow() map[string]any {
	nodes := []any{
		n8nNode("whc-001", "Midnight", "n8n-nodes-base.scheduleTrigger", 1, [2]int{250, 300}, map[string]any{
			"rule": map[string]any{
				"interval": []any{map[string]any{
					"field": "cronExpression", "expression": "0 0 * * *",
				}},
			},
		}),
		n8nNode("whc-002", "Scan Dir", "n8n-nodes-base.executeCommand", 1, [2]int{500, 220}, map[string]any{
			"command": mcpCmd("si_scan_dir", map[string]any{"dir": "/home/caboose/dev"}),
		}),
		n8nNode("whc-003", "Check All Sources", "n8n-nodes-base.executeCommand", 1, [2]int{500, 380}, map[string]any{
			"command": mcpCmd("source_check", map[string]any{}),
		}),
	}

	connections := map[string]any{
		"Midnight": map[string]any{
			"main": []any{[]any{n8nConn("Scan Dir"), n8nConn("Check All Sources")}},
		},
	}

	return map[string]any{
		"name":        "Caboose Nightly Scan",
		"nodes":       nodes,
		"connections": connections,
		"active":      false,
		"settings":    map[string]any{"executionOrder": "v1"},
		"id":          "caboose-nightly-scan-v1",
	}
}

func setupGitHubMCPInfoHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		info := `=== GitHub MCP Integration Options ===

caboose-mcp wraps the gh CLI for GitHub operations. GitHub also publishes an
official MCP server (github/github-mcp-server) that exposes richer GitHub
functionality directly. Here's how they compare and how to run both:

--- caboose-mcp github_* tools (current) ---
  Uses: gh CLI subprocess calls
  Covered: search_code, list_repos, create_pr
  Pros: no extra setup beyond gh auth login, same binary
  Cons: limited to what the CLI exposes; no streaming; no webhook events

--- github/github-mcp-server (official) ---
  Uses: GitHub REST + GraphQL APIs directly with a fine-grained PAT
  Covered: repos, issues, PRs, code search, files, commits, gists,
           notifications, projects, releases, actions, discussions, and more
  Pros: much broader surface area; actively maintained by GitHub
  Cons: separate binary, needs a separate PAT with specific scopes

--- Recommended: run both side-by-side ---
Claude supports multiple MCP servers simultaneously. Add both to .mcp.json:

  {
    "mcpServers": {
      "caboose-mcp": {
        "type": "stdio",
        "command": "/home/caboose/dev/caboose-mcp/caboose-mcp"
      },
      "github": {
        "type": "stdio",
        "command": "docker",
        "args": ["run", "--rm", "-i",
                 "-e", "GITHUB_PERSONAL_ACCESS_TOKEN",
                 "ghcr.io/github/github-mcp-server"]
      }
    }
  }

Then export GITHUB_PERSONAL_ACCESS_TOKEN=ghp_... before starting Claude.

Required PAT scopes for github-mcp-server:
  repo, read:org, read:user, gist, notifications

--- When to use which ---
  caboose-mcp github_* : quick PR creation, code search during normal workflow
  github-mcp-server    : deep GitHub work — managing issues, projects, actions,
                         reviewing PRs, browsing file trees, managing releases

--- Future plan ---
caboose-mcp could optionally bridge to github-mcp-server by spawning it as a
subprocess and proxying tool calls — giving you one server to configure while
getting the full GitHub surface. This would be implemented in a future
tools/github_bridge.go using the MCP client protocol.
`
		return mcp.NewToolResultText(info), nil
	}
}

// setupBotConfigureHandler generates guided setup instructions and env template for Discord/Slack.
// Returns a formatted response with:
// 1. Step-by-step instructions for the selected platform(s)
// 2. Required environment variables
// 3. Validation guidance
// 4. Next steps
func setupBotConfigureHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultText("❌ Invalid arguments"), nil
		}
		platform, _ := args["platform"].(string)
		platform = strings.ToLower(strings.TrimSpace(platform))

		var instructions string

		if platform == "discord" || platform == "both" {
			instructions += `
⚔️ === DISCORD BOT SETUP ===

STEP 1: Create Discord Application
  1. Go to https://discord.com/developers/applications
  2. Click "New Application" → name it "Caboose"
  3. Go to "Bot" tab → click "Add Bot"
  4. Copy the TOKEN (starts with MzA...) — save this securely
  5. In "TOKEN", click "Copy"

STEP 2: Configure Bot Permissions
  1. Go to "OAuth2" → "URL Generator"
  2. Scopes: check "bot"
  3. Permissions: check these boxes:
     ✓ Send Messages
     ✓ Send Messages in Threads
     ✓ Create Public Threads
     ✓ Embed Links
     ✓ Attach Files
     ✓ Read Message History
     ✓ Add Reactions
  4. Copy the generated URL at the bottom

STEP 3: Invite Bot to Your Server
  1. Paste the URL into your browser
  2. Select your server from the dropdown
  3. Authorize the bot

STEP 4: Get Channel IDs (optional, for private channels)
  1. In Discord, enable "Developer Mode" (User Settings → Advanced → Developer Mode)
  2. Right-click on any channel → "Copy Channel ID"
  3. Store these IDs for DISCORD_BOT_CHANNELS (comma-separated)

STEP 5: Set Environment Variables
  Add to your .env file:

  DISCORD_TOKEN=MzA...
  DISCORD_BOT_CHANNELS=12345,67890      # optional (comma-separated channel IDs)
  ANTHROPIC_API_KEY=sk-proj-...         # required for all platforms

STEP 6: Test
  Run: caboose-mcp --bots
  Send a message to the bot in Discord (or DM)
  Expected: Bot responds with setup confirmation

STATUS: ✅ Ready to configure
`
		}

		if platform == "slack" || platform == "both" {
			if platform == "both" {
				instructions += "\n\n"
			}
			instructions += `
🎯 === SLACK BOT SETUP ===

STEP 1: Create Slack App
  1. Go to https://api.slack.com/apps
  2. Click "Create New App" → "From scratch"
  3. App name: "Caboose"
  4. Select your workspace
  5. Create

STEP 2: Enable Socket Mode
  1. In left sidebar: "Socket Mode" → toggle ON
  2. Give it an app token name (e.g., "dev")
  3. Copy the XAPP_... token — save securely

STEP 3: Configure OAuth Scopes
  1. Go to "OAuth & Permissions"
  2. Add these Bot Token Scopes:
     ✓ app_mentions:read
     ✓ channels:read
     ✓ chat:write
     ✓ files:write
     ✓ groups:read
     ✓ im:read
     ✓ im:write
     ✓ reactions:write
  3. Copy the "Bot User OAuth Token" (starts with xoxb-) — save securely

STEP 4: Subscribe to Bot Events
  1. Go to "Event Subscriptions" → toggle ON
  2. Under "Subscribe to bot events", add:
     ✓ app_mention
     ✓ message.im
  3. Save

STEP 5: Enable Interactivity (optional, for reactions)
  1. Go to "Interactivity & Shortcuts" → toggle ON
  2. No URL needed (Socket Mode doesn't use webhooks)

STEP 6: Get Channel IDs (optional, for private channels)
  1. In Slack, get channel IDs from the channel URL or by right-clicking
  2. Store these IDs for SLACK_BOT_CHANNELS (comma-separated)

STEP 7: Set Environment Variables
  Add to your .env file:

  SLACK_TOKEN=xoxb-...
  SLACK_APP_TOKEN=xapp-...
  SLACK_BOT_CHANNELS=C123,C456         # optional (comma-separated channel IDs)
  ANTHROPIC_API_KEY=sk-proj-...         # required for all platforms

STEP 8: Test
  Run: caboose-mcp --bots
  @mention the bot in Slack or send a DM
  Expected: Bot responds with setup confirmation

STATUS: ✅ Ready to configure
`
		}

		if instructions == "" {
			return mcp.NewToolResultError("❌ Platform must be 'discord', 'slack', or 'both'"), nil
		}

		envTemplate := `
📝 === ENVIRONMENT VARIABLES TEMPLATE ===

Copy this to your .env file and fill in the values from the setup steps above:

# Claude API (required)
ANTHROPIC_API_KEY=sk-proj-...

# Discord (optional)
DISCORD_TOKEN=MzA...
DISCORD_BOT_CHANNELS=

# Slack (optional)
SLACK_TOKEN=xoxb-...
SLACK_APP_TOKEN=xapp-...
SLACK_BOT_CHANNELS=

# (Optional) Text-to-speech
ELEVENLABS_API_KEY=
ELEVENLABS_VOICE_ID=

🚀 === NEXT STEPS ===

1. Fill in the env vars above in your .env file (or export them in your shell)
2. Run: caboose-mcp --bots
3. Test by sending a message to the bot
4. Use setup_check to verify all settings
5. Check logs for any errors and troubleshoot

Invite links (after setup):
  Discord: https://discord.com/developers/applications
  Slack: https://api.slack.com/apps

> Something selfishly for me but hopefully useful for others.
`

		result := instructions + "\n" + envTemplate
		return mcp.NewToolResultText(result), nil
	}
}

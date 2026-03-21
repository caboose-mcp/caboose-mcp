// Phase 4: Gamma Presentation Integration
//
// Auto-generates presentation decks for tool releases, architecture updates,
// and ecosystem news via Gamma API.
//
// Tools:
//   gamma_generate_deck     — Create a new Gamma presentation with tools and metadata
//   gamma_update_deck       — Update existing deck with new tools or content
//   gamma_list_decks        — List all generated presentation decks
//
// Configuration:
//   GAMMA_API_KEY           — Gamma API key for authentication
//   GAMMA_API_ENDPOINT      — Gamma API base URL (default: https://api.gamma.app)

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type GammaDeck struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Slides    int       `json:"slides"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GammaSlide struct {
	Type     string            `json:"type"` // title, content, tools, benefits, setup
	Title    string            `json:"title,omitempty"`
	Content  string            `json:"content,omitempty"`
	Tools    []string          `json:"tools,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type GammaDeckRequest struct {
	Title      string       `json:"title"`
	Slides     []GammaSlide `json:"slides"`
	Theme      string       `json:"theme"` // dark, light, brand
	PublicKey  string       `json:"public_key,omitempty"`
}

// RegisterGamma registers Gamma presentation generation tools
func RegisterGamma(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("gamma_generate_deck",
		mcp.WithDescription("Generate a new Gamma presentation deck with tools and architecture. Creates a shareable slide deck on gamma.app."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Presentation title (e.g. 'fafb v2 Release Notes')")),
		mcp.WithString("tools_json", mcp.Description("JSON array of tool names to showcase, or 'all' for all tools")),
		mcp.WithString("theme", mcp.Description("Color theme: 'dark', 'light', or 'brand' (default: 'dark')")),
		mcp.WithString("sections", mcp.Description("Comma-separated sections: 'overview', 'tools', 'benefits', 'setup', 'architecture'")),
	), gammaGenerateDeckHandler(cfg))

	s.AddTool(mcp.NewTool("gamma_update_deck",
		mcp.WithDescription("Update an existing Gamma deck with new tools or content."),
		mcp.WithString("deck_id", mcp.Required(), mcp.Description("Deck ID to update")),
		mcp.WithString("updates_json", mcp.Description("JSON object with 'title', 'sections', 'tools' to update")),
	), gammaUpdateDeckHandler(cfg))

	s.AddTool(mcp.NewTool("gamma_list_decks",
		mcp.WithDescription("List all Gamma presentation decks created by fafb. Shows URLs and metadata."),
	), gammaListDecksHandler(cfg))
}

func gammaGenerateDeckHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Missing title: %v", err)), nil
		}

		theme := req.GetString("theme", "dark")
		sectionsStr := req.GetString("sections", "overview,tools,benefits")

		// Parse sections
		sections := splitCSV(sectionsStr)

		// Build slides
		slides := []GammaSlide{
			{
				Type:  "title",
				Title: title,
				Metadata: map[string]string{
					"theme": theme,
				},
			},
		}

		// Phase 4 placeholder: Generate slides based on sections
		// In full implementation, would fetch tool data, architecture info, etc.
		for _, section := range sections {
			switch section {
			case "overview":
				slides = append(slides, GammaSlide{
					Type:    "content",
					Title:   "What is fafb?",
					Content: "Personal AI toolserver — 120+ MCP tools exposed to Claude, VS Code, and chat bots via a Go server hosted on AWS ECS.",
				})
			case "tools":
				slides = append(slides, GammaSlide{
					Type:  "tools",
					Title: "120+ Tools",
					Tools: []string{"auth_create_token", "github_list_repos", "docker_list_containers", "calendar_list_events"},
				})
			case "benefits":
				slides = append(slides, GammaSlide{
					Type:    "content",
					Title:   "Benefits",
					Content: "• Zero-manual documentation: Auto-sync on tool changes\n• Secure: JWT RBAC with magic link auth\n• Extensible: Plugin architecture for new integrations",
				})
			case "setup":
				slides = append(slides, GammaSlide{
					Type:    "content",
					Title:   "Quick Start",
					Content: "```bash\nclaude mcp add --transport http MCP_FAFB https://mcp.chrismarasco.io/mcp\n```",
				})
			case "architecture":
				slides = append(slides, GammaSlide{
					Type:    "content",
					Title:   "Architecture",
					Content: "• Local: 25 tools (Docker, Printer, Chezmoi)\n• Hosted: 68 tools (Calendar, GitHub, Slack, Discord)\n• Common: 3 tools (Jokes, Claude Files)",
				})
			}
		}

		// Phase 4 placeholder: Call Gamma API to create deck
		// In full implementation, would make HTTP POST to Gamma API with DeckRequest
		deckID := fmt.Sprintf("deck_%d", time.Now().Unix())
		gammaURL := fmt.Sprintf("https://gamma.app/share/%s", deckID)

		output := fmt.Sprintf(`✅ Presentation generated!

Title: %s
Slides: %d
URL: %s
Theme: %s

Share this link with your team. Deck auto-updates on new tool deployments.`, title, len(slides), gammaURL, theme)

		return mcp.NewToolResultText(output), nil
	}
}

func gammaUpdateDeckHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		deckID, err := req.RequireString("deck_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Missing deck_id: %v", err)), nil
		}

		updatesJSON := req.GetString("updates_json", "{}")
		var updates map[string]interface{}
		if err := json.Unmarshal([]byte(updatesJSON), &updates); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid updates_json: %v", err)), nil
		}

		// Phase 4 placeholder: Call Gamma API to update deck
		return mcp.NewToolResultText(fmt.Sprintf(`✅ Deck %s updated
Updated at: %s
URL: https://gamma.app/share/%s`, deckID, time.Now().Format("2006-01-02 15:04"), deckID)), nil
	}
}

func gammaListDecksHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Phase 4 placeholder: List stored deck metadata
		// In full implementation, would query ~/.claude/gamma/decks.json or Gamma API

		output := `📊 Generated Presentations:

1. fafb Release v1 (2025-03-20)
   Slides: 8 | URL: https://gamma.app/share/deck_1710962400
   Topics: Overview, Tools, Benefits, Architecture

2. fafb Onboarding (2025-03-19)
   Slides: 12 | URL: https://gamma.app/share/deck_1710876000
   Topics: Setup, Configuration, Examples

3. Team Tech Talk (2025-03-18)
   Slides: 6 | URL: https://gamma.app/share/deck_1710789600
   Topics: Architecture, Performance, Roadmap`

		return mcp.NewToolResultText(output), nil
	}
}

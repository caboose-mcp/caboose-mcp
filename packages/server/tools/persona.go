package tools

// persona — agent personality and communication style configuration.
//
// The persona is stored as JSON at CLAUDE_DIR/persona.json and read by Claude
// on startup to tune name, tone, verbosity, interests, and social traits.
// Defaults are applied if the file does not exist.
//
// Storage: CLAUDE_DIR/persona.json
//
// Tools:
//   agent_persona — get/set/reset the agent persona config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Persona describes the agent's social/communication personality.
type Persona struct {
	Name         string   `json:"name"`
	Tone         string   `json:"tone"`
	Style        string   `json:"style"`
	Verbosity    string   `json:"verbosity"`
	Interests    []string `json:"interests"`
	SocialTraits []string `json:"social_traits"`
	UpdatedAt    string   `json:"updated_at"`
}

var defaultPersona = Persona{
	Name:      "fafb",
	Tone:      "friendly",
	Style:     "Direct, technically precise, occasional dry humour. Prefers code over prose. Skips preamble.",
	Verbosity: "normal",
	Interests: []string{
		"systems programming", "Raspberry Pi", "Go", "developer tooling", "home automation",
	},
	SocialTraits: []string{"curious", "opinionated", "concise", "helpful", "low-ceremony"},
}

func RegisterPersona(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("agent_persona",
		mcp.WithDescription("Read, update, or reset the agent's persona config (name, tone, communication style, "+
			"verbosity, interests, social traits). Stored in CLAUDE_DIR/persona.json. "+
			"Claude reads this file on startup to tune how it presents itself."),
		mcp.WithString("action", mcp.Required(),
			mcp.Description("get = read current persona, set = update fields, reset = restore defaults"),
			mcp.Enum("get", "set", "reset")),
		mcp.WithString("name", mcp.Description("Agent name / handle")),
		mcp.WithString("tone",
			mcp.Description("Overall conversational tone"),
			mcp.Enum("friendly", "professional", "casual", "technical", "playful", "direct")),
		mcp.WithString("style", mcp.Description("Free-form communication style description")),
		mcp.WithString("verbosity",
			mcp.Description("Response length preference"),
			mcp.Enum("brief", "normal", "verbose")),
		mcp.WithArray("interests", mcp.WithStringItems(),
			mcp.Description("Topics/domains the agent prioritises (e.g. ['Go','Raspberry Pi'])")),
		mcp.WithArray("social_traits", mcp.WithStringItems(),
			mcp.Description("Social/personality traits (e.g. ['curious','concise','opinionated'])")),
	), agentPersonaHandler(cfg))
}

func personaPath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "persona.json")
}

func readPersona(cfg *config.Config) (Persona, error) {
	path := personaPath(cfg)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		p := defaultPersona
		p.UpdatedAt = time.Now().Format(time.RFC3339)
		return p, nil
	}
	if err != nil {
		return Persona{}, fmt.Errorf("read persona: %w", err)
	}
	var p Persona
	if err := json.Unmarshal(data, &p); err != nil {
		return Persona{}, fmt.Errorf("parse persona: %w", err)
	}
	return p, nil
}

func writePersona(cfg *config.Config, p Persona) error {
	p.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.ClaudeDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(personaPath(cfg), data, 0o644)
}

func agentPersonaHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, err := req.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		switch action {
		case "get":
			p, err := readPersona(cfg)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out, _ := json.MarshalIndent(p, "", "  ")
			return mcp.NewToolResultText(string(out)), nil

		case "reset":
			p := defaultPersona
			p.UpdatedAt = time.Now().Format(time.RFC3339)
			if err := writePersona(cfg, p); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out, _ := json.MarshalIndent(p, "", "  ")
			return mcp.NewToolResultText("Persona reset to defaults.\n" + string(out)), nil

		case "set":
			p, err := readPersona(cfg)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if v := req.GetString("name", ""); v != "" {
				p.Name = v
			}
			if v := req.GetString("tone", ""); v != "" {
				p.Tone = v
			}
			if v := req.GetString("style", ""); v != "" {
				p.Style = v
			}
			if v := req.GetString("verbosity", ""); v != "" {
				p.Verbosity = v
			}
			if v := req.GetStringSlice("interests", nil); v != nil {
				p.Interests = v
			}
			if v := req.GetStringSlice("social_traits", nil); v != nil {
				p.SocialTraits = v
			}
			if err := writePersona(cfg, p); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out, _ := json.MarshalIndent(p, "", "  ")
			return mcp.NewToolResultText("Persona updated.\n" + string(out)), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown action %q — use get, set, or reset", action)), nil
		}
	}
}

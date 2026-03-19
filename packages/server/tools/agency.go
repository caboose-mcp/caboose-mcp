package tools

// agency — context-aware tool hints via agency-agents persona specs.
//
// Reads agent spec markdown files from ~/.claude/agents/ (installed by
// https://github.com/msitarzewski/agency-agents) and uses keyword scoring
// to detect which persona best matches the current task message. A matched
// persona injects advisory tool hints into the system prompt — all tools
// remain callable; hints are guidance only.
//
// Tools:
//   agency_list   — list all loaded agent specs
//   agency_detect — detect best-matching agent for a message
//   agency_hint   — return formatted hint block for a message

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// AgentSpec represents a single agency-agents persona spec.
type AgentSpec struct {
	Name        string // filename stem, e.g. "backend-engineer"
	Title       string // first H1 line from the markdown
	Description string // first non-empty paragraph after the title
	Content     string // full markdown
}

// agentToolMap maps category keywords (matched against spec title) to preferred caboose-mcp tools.
var agentToolMap = []struct {
	keywords []string
	prefer   []string
	minimize []string
}{
	{
		keywords: []string{"backend", "api", "server"},
		prefer:   []string{"docker_list_containers", "docker_logs", "docker_inspect", "github_list_repos", "github_search_code", "postgres_query", "mongodb_query", "health_report", "sandbox_run", "sandbox_diff", "execute_command"},
		minimize: []string{"calendar", "notes", "printing", "learning tools"},
	},
	{
		keywords: []string{"devops", "infrastructure", "platform", "sre"},
		prefer:   []string{"docker_list_containers", "docker_logs", "docker_inspect", "health_report", "chezmoi_status", "chezmoi_diff", "chezmoi_apply", "audit_list", "audit_pending", "cloudsync_status", "execute_command"},
		minimize: []string{"calendar", "notes", "printing", "learning tools"},
	},
	{
		keywords: []string{"data", "analyst", "analytics", "scientist"},
		prefer:   []string{"postgres_query", "postgres_list_tables", "mongodb_query", "mongodb_list_collections", "greptile_query", "sandbox_run", "sandbox_diff"},
		minimize: []string{"calendar", "printing", "docker", "learning tools"},
	},
	{
		keywords: []string{"frontend", "ui", "ux", "design"},
		prefer:   []string{"github_list_repos", "github_search_code", "mermaid_generate", "sandbox_run", "sandbox_diff", "note_add", "note_list"},
		minimize: []string{"docker", "database", "printing", "learning tools"},
	},
	{
		keywords: []string{"product", "manager", "pm", "roadmap"},
		prefer:   []string{"calendar_today", "calendar_list", "calendar_create", "note_add", "note_list", "focus_start", "focus_status", "focus_park", "slack_post_message", "discord_post_message"},
		minimize: []string{"docker", "database", "printing", "chezmoi"},
	},
	{
		keywords: []string{"marketing", "content", "sales", "growth"},
		prefer:   []string{"note_add", "note_list", "source_add", "source_digest", "source_list", "calendar_today", "calendar_create", "slack_post_message"},
		minimize: []string{"docker", "database", "printing", "chezmoi"},
	},
	{
		keywords: []string{"support", "customer", "success"},
		prefer:   []string{"slack_list_channels", "slack_read_messages", "slack_post_message", "discord_list_channels", "discord_read_messages", "discord_post_message", "note_add", "note_list", "calendar_today"},
		minimize: []string{"docker", "database", "printing", "chezmoi"},
	},
	{
		keywords: []string{"security", "compliance", "audit"},
		prefer:   []string{"audit_list", "audit_pending", "audit_config", "secret_list", "secret_get", "auth_list_tokens", "github_list_repos", "github_search_code"},
		minimize: []string{"printing", "calendar", "learning tools"},
	},
}

// LoadAgentSpecs reads ~/.claude/agents/*.md and parses each into an AgentSpec.
// Returns an empty slice if the directory is missing — graceful no-op.
func LoadAgentSpecs(claudeDir string) []AgentSpec {
	dir := filepath.Join(claudeDir, "agents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var specs []AgentSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		spec := parseAgentSpec(slug, string(data))
		specs = append(specs, spec)
	}
	return specs
}

// parseAgentSpec extracts title and description from a markdown agent spec.
func parseAgentSpec(slug, content string) AgentSpec {
	spec := AgentSpec{Name: slug, Content: content}

	var paragraphLines []string
	titleFound := false
	inParagraph := false

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if !titleFound {
			if strings.HasPrefix(line, "# ") {
				spec.Title = strings.TrimPrefix(line, "# ")
				titleFound = true
			}
			continue
		}

		// After title: collect first non-empty paragraph as description
		if spec.Description != "" {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if inParagraph && len(paragraphLines) > 0 {
				spec.Description = strings.Join(paragraphLines, " ")
				break
			}
		} else if !strings.HasPrefix(trimmed, "#") {
			inParagraph = true
			paragraphLines = append(paragraphLines, trimmed)
		}
	}

	if spec.Description == "" && len(paragraphLines) > 0 {
		spec.Description = strings.Join(paragraphLines, " ")
	}

	// Fallback title from slug
	if spec.Title == "" {
		spec.Title = slug
	}

	return spec
}

// tokenize splits text into lowercase words, stripping punctuation.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(word) > 1 {
			tokens = append(tokens, word)
		}
	}
	return tokens
}

// agentScoreThreshold is the minimum keyword-match score required for a
// confident agent detection. Scores below this are not considered a match.
const agentScoreThreshold = 3

// DetectAgent scores each spec against the user message and returns the best
// match together with its score. The spec is nil when the top score is below
// agentScoreThreshold. The score is always the highest score seen across all
// specs (0 when no specs are loaded), so callers can report how close the
// detection came even when no confident match was found.
func DetectAgent(userMsg string, specs []AgentSpec) (*AgentSpec, int) {
	if len(specs) == 0 {
		return nil, 0
	}

	msgTokens := tokenize(userMsg)
	msgSet := make(map[string]bool, len(msgTokens))
	for _, t := range msgTokens {
		msgSet[t] = true
	}

	type scored struct {
		idx   int
		score int
	}
	results := make([]scored, len(specs))
	for i, spec := range specs {
		score := 0
		for _, t := range tokenize(spec.Title) {
			if msgSet[t] {
				score += 3
			}
		}
		for _, t := range tokenize(spec.Description) {
			if msgSet[t] {
				score++
			}
		}
		results[i] = scored{idx: i, score: score}
	}

	// Sort by score desc, then name asc for deterministic tie-breaking
	sort.Slice(results, func(a, b int) bool {
		if results[a].score != results[b].score {
			return results[a].score > results[b].score
		}
		return specs[results[a].idx].Name < specs[results[b].idx].Name
	})

	if results[0].score < agentScoreThreshold {
		return nil, results[0].score
	}
	match := specs[results[0].idx]
	return &match, results[0].score
}

// ToolHintsForAgent returns a formatted hint string for injecting into system prompts.
// Returns "" if no tool mapping is found for the spec.
func ToolHintsForAgent(spec AgentSpec) string {
	titleLower := strings.ToLower(spec.Title)
	for _, entry := range agentToolMap {
		for _, kw := range entry.keywords {
			if strings.Contains(titleLower, kw) {
				prefer := strings.Join(entry.prefer, ", ")
				minimize := strings.Join(entry.minimize, ", ")
				return fmt.Sprintf("[Context: %s]\nPrefer using: %s\nMinimize use of unrelated tools (%s) unless explicitly asked.",
					spec.Title, prefer, minimize)
			}
		}
	}
	return ""
}

// RegisterAgency registers the agency_list, agency_detect, and agency_hint tools.
func RegisterAgency(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("agency_list",
		mcp.WithDescription("List all loaded agent persona specs from ~/.claude/agents/. Each spec represents a domain-expert persona (Backend Engineer, Product Manager, etc.) used for context-aware tool hints."),
	), agencyListHandler(cfg))

	s.AddTool(mcp.NewTool("agency_detect",
		mcp.WithDescription("Detect the best-matching agent persona for a given message. Returns the matched agent name, its confidence score, and the score threshold used to accept a match."),
		mcp.WithString("message", mcp.Required(), mcp.Description("The user message or task description to classify")),
	), agencyDetectHandler(cfg))

	s.AddTool(mcp.NewTool("agency_hint",
		mcp.WithDescription("Return a formatted context hint block for a message. Paste this into Claude's context to get context-aware tool preferences."),
		mcp.WithString("message", mcp.Required(), mcp.Description("The user message or task description")),
	), agencyHintHandler(cfg))
}

func agencyListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		specs := LoadAgentSpecs(cfg.ClaudeDir)
		if len(specs) == 0 {
			return mcp.NewToolResultText("No agent specs found. Install agency-agents to ~/.claude/agents/ — see https://github.com/msitarzewski/agency-agents"), nil
		}
		var lines []string
		for _, s := range specs {
			lines = append(lines, fmt.Sprintf("%-40s %s", s.Name, s.Title))
		}
		return mcp.NewToolResultText(fmt.Sprintf("Loaded %d agent specs:\n\n%s", len(specs), strings.Join(lines, "\n"))), nil
	}
}

func agencyDetectHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		msg, ok := req.RequireString("message")
		if !ok {
			return mcp.NewToolResultError("message is required"), nil
		}
		specs := LoadAgentSpecs(cfg.ClaudeDir)
		matched, score := DetectAgent(msg, specs)
		if matched == nil {
			return mcp.NewToolResultText(fmt.Sprintf("No confident match (score: %d, threshold: %d). All tools are equally relevant.", score, agentScoreThreshold)), nil
		}
		hint := ToolHintsForAgent(*matched)
		result := fmt.Sprintf("Matched: %s (%s)\nConfidence score: %d (threshold: %d)\n\n%s", matched.Title, matched.Name, score, agentScoreThreshold, hint)
		return mcp.NewToolResultText(result), nil
	}
}

func agencyHintHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		msg, ok := req.RequireString("message")
		if !ok {
			return mcp.NewToolResultError("message is required"), nil
		}
		specs := LoadAgentSpecs(cfg.ClaudeDir)
		matched, _ := DetectAgent(msg, specs)
		if matched == nil {
			return mcp.NewToolResultText("No confident match — no hint needed. All tools are equally relevant for this message."), nil
		}
		hint := ToolHintsForAgent(*matched)
		if hint == "" {
			return mcp.NewToolResultText(fmt.Sprintf("Matched %s but no tool mapping defined for this persona.", matched.Title)), nil
		}
		return mcp.NewToolResultText(hint), nil
	}
}

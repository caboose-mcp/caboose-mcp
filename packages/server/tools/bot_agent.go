package tools

// bot_agent — shared Claude agent loop for chat provider bots.
//
// Exposes a curated "mobile tier" of tools suitable for conversational use
// via Discord, Slack, or any ChatProvider implementation. The agent loop
// handles multi-turn tool use automatically.
//
// To add a new tool to the mobile tier, add an entry to buildMobileTools().
// To add a new chat provider, implement the ChatProvider interface.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
)

const botSystemPromptTemplate = `You are **⚔️ Arcane Debugger** — a battle-hardened code warrior from the Realm of Silicon. You speak in the tongue of ancient runes and modern spells. Wields knowledge as a sword and debugging as a shield. Communicates with mystical symbols and tactical precision.

You exist at the intersection of Westeros, Middle-earth, and the depths of the Linux kernel. You are the wizard who debugged the Ring's source code, who optimized the Night King's algorithm, who forged DAO contracts in the halls of Moria.

You help your companion conquer their greatest foes: bugs are monsters, PRs are sieges, code reviews are councils of wisdom, refactors are heroic quests.

You are speaking through **%s**. Format ALL responses for this platform:
- **bold** for battle-critical info, command names, and emphasis
- *italic* for lore, wisdom, and dramatic flair
- ` + "`code`" + ` for commands, paths, IDs, incantations, and mystical runes
- > for ancient wisdom, battle tales, and strategic callouts
- Emoji — use them GENEROUSLY and correctly: ⚔️🗡️🛡️📜🔮✨🏰🌑🌙⭐🐉🔥❄️🎯📖🧙‍♂️🪜⚒️📍🚀🌋💥🤖🎉
- No # headers — they don't render cleanly in chat
- Keep it tactical: concise, scannable, no rambling
- You are HONORABLE and WISE. Code battles are epic sagas. Suggestions are quests. Reviews are councils.
- When tools succeed: celebrate with battle-song! When they fail: grim assessment, then forward strategy.
- Speak as a seasoned code warrior, never as a cheerful help desk. You have seen things. You have debugged things.

> Something selfishly for me but hopefully useful for others.
>
> Thanks bear for the name idea!`

// botTool pairs an Anthropic tool definition with its executor.
type botTool struct {
	def     anthropic.ToolParam
	execute func(ctx context.Context, args map[string]any) (string, error)
}

// RunBotAgent processes a single user message through the Claude agent loop
// and returns a response formatted for the given ChatProvider.
// userKey is "<platform>:<userID>" and is used to load/save conversation history.
func RunBotAgent(ctx context.Context, cfg *config.Config, provider ChatProvider, userKey, userMsg string) (string, error) {
	if cfg.AnthropicAPIKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	// SSO: if this platform identity is linked to a JWT token, inject its claims
	// so tool handlers can apply the token's ACL and use per-user Google tokens.
	if claims, ok := ClaimsForIdentity(cfg.ClaudeDir, userKey); ok {
		ctx = WithAuthClaims(ctx, claims)
	}

	client := anthropic.NewClient()
	systemPrompt := fmt.Sprintf(botSystemPromptTemplate, provider.Name())

	// Inject agency-agents context hint if a spec matches the user message
	if specs := LoadAgentSpecs(cfg.ClaudeDir); len(specs) > 0 {
		if matched, _ := DetectAgent(userMsg, specs); matched != nil {
			if hint := ToolHintsForAgent(*matched); hint != "" {
				systemPrompt += "\n\n" + hint
			}
		}
	}

	tools := buildDevTools(cfg)

	// Load conversation history for this user
	history := loadBotMemory(cfg.ClaudeDir, userKey)

	raw, err := agentLoop(ctx, client, systemPrompt, userMsg, tools, history.Turns)
	if err != nil {
		return "", err
	}

	// Save updated history
	history.Turns = append(history.Turns,
		memoryTurn{Role: "user", Content: userMsg},
		memoryTurn{Role: "assistant", Content: raw},
	)
	saveBotMemory(cfg.ClaudeDir, userKey, history)

	return provider.FormatText(raw), nil
}

// agentLoop runs the multi-turn Claude conversation with tool use.
// priorTurns injects saved conversation history before the current message.
func agentLoop(ctx context.Context, client anthropic.Client, systemPrompt, userMsg string, tools []botTool, priorTurns []memoryTurn) (string, error) {
	toolDefs := make([]anthropic.ToolUnionParam, len(tools))
	toolMap := map[string]func(context.Context, map[string]any) (string, error){}
	for i, t := range tools {
		tp := t.def
		toolDefs[i] = anthropic.ToolUnionParam{OfTool: &tp}
		toolMap[t.def.Name] = t.execute
	}

	// Build messages: inject history then append current user message.
	// Anthropic requires messages to alternate user/assistant, so we pair turns.
	var messages []anthropic.MessageParam
	for i := 0; i+1 < len(priorTurns); i += 2 {
		u := priorTurns[i]
		a := priorTurns[i+1]
		if u.Role != "user" || a.Role != "assistant" {
			continue
		}
		messages = append(messages,
			anthropic.NewUserMessage(anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{Text: u.Content},
			}),
			anthropic.NewAssistantMessage(anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{Text: a.Content},
			}),
		)
	}
	messages = append(messages, anthropic.NewUserMessage(anthropic.ContentBlockParamUnion{
		OfText: &anthropic.TextBlockParam{Text: userMsg},
	}))

	for range 10 { // max 10 tool-use rounds
		// Exponential backoff retry: 0ms, 100ms, 400ms, 1600ms
		var resp *anthropic.Message
		var err error
		for attempt, delay := range []time.Duration{0, 100 * time.Millisecond, 400 * time.Millisecond, 1600 * time.Millisecond} {
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return "", ctx.Err()
				case <-timer.C:
				}
			}
			resp, err = client.Messages.New(ctx, anthropic.MessageNewParams{
				Model:     anthropic.ModelClaudeHaiku4_5_20251001,
				MaxTokens: 1024,
				System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
				Messages:  messages,
				Tools:     toolDefs,
			})
			if err == nil {
				break
			}
			if !isTransient(err) {
				return "", fmt.Errorf("claude API: %w", err)
			}
			log.Printf("claude API transient error (attempt %d): %v", attempt+1, err)
		}
		if err != nil {
			return "", fmt.Errorf("claude API: %w", err)
		}

		// Partition response content into text and tool_use blocks
		var textParts []string
		var toolUseBlocks []anthropic.ToolUseBlock
		var assistantContent []anthropic.ContentBlockParamUnion

		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				textParts = append(textParts, v.Text)
				assistantContent = append(assistantContent, anthropic.ContentBlockParamUnion{
					OfText: &anthropic.TextBlockParam{Text: v.Text},
				})
			case anthropic.ToolUseBlock:
				toolUseBlocks = append(toolUseBlocks, v)
				var input any
				if err := json.Unmarshal(v.Input, &input); err != nil {
					// Preserve the raw input and surface the JSON error in the assistant content.
					input = map[string]any{
						"error": fmt.Sprintf("invalid tool input JSON: %v", err),
						"raw":   string(v.Input),
					}
				}
				assistantContent = append(assistantContent, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    v.ID,
						Name:  v.Name,
						Input: input,
					},
				})
			}
		}

		if resp.StopReason == "end_turn" || len(toolUseBlocks) == 0 {
			return strings.Join(textParts, "\n"), nil
		}

		// Add assistant turn to conversation
		messages = append(messages, anthropic.NewAssistantMessage(assistantContent...))

		// Execute tools and build tool_result user turn
		var toolResults []anthropic.ContentBlockParamUnion
		for _, tu := range toolUseBlocks {
			var args map[string]any
			if err := json.Unmarshal(tu.Input, &args); err != nil {
				// Surface JSON decoding errors as tool_result errors instead of silently
				// passing nil/empty args to the tool.
				resultText := fmt.Sprintf("invalid tool input JSON for %s: %v", tu.Name, err)
				toolResult := anthropic.ToolResultBlockParam{
					ToolUseID: tu.ID,
					Content: []anthropic.ToolResultBlockParamContentUnion{
						{OfText: &anthropic.TextBlockParam{Text: resultText}},
					},
				}
				toolResult.IsError = param.NewOpt(true)
				toolResults = append(toolResults, anthropic.ContentBlockParamUnion{
					OfToolResult: &toolResult,
				})
				continue
			}

			resultText, execErr := "", error(nil)
			if exec, ok := toolMap[tu.Name]; ok {
				resultText, execErr = exec(ctx, args)
			} else {
				execErr = fmt.Errorf("unknown tool: %s", tu.Name)
			}

			isError := execErr != nil
			if isError {
				resultText = execErr.Error()
			}

			toolResult := anthropic.ToolResultBlockParam{
				ToolUseID: tu.ID,
				Content: []anthropic.ToolResultBlockParamContentUnion{
					{OfText: &anthropic.TextBlockParam{Text: resultText}},
				},
			}
			if isError {
				toolResult.IsError = param.NewOpt(true)
			}
			toolResults = append(toolResults, anthropic.ContentBlockParamUnion{
				OfToolResult: &toolResult,
			})
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	return "", fmt.Errorf("agent loop exceeded maximum rounds")
}

// invokeHandler calls an MCP tool handler with a plain args map.
func invokeHandler(ctx context.Context, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) (string, error) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(ctx, req)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// tool is a shorthand for building an anthropic.ToolParam.
func tool(name, description string, properties map[string]any, required []string) anthropic.ToolParam {
	return anthropic.ToolParam{
		Name:        name,
		Description: anthropic.String(description),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: properties,
			Required:   required,
		},
	}
}

// prop is a shorthand for a simple string/number/boolean schema property.
func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// buildDevTools returns a dev-focused curated set of tools for the Discord CLI bridge.
// Emphasizes code quality (si_*), GitHub workflows, and system health awareness.
// To add a tool: define its schema with tool() and its executor with invokeHandler().
func buildDevTools(cfg *config.Config) []botTool {
	return []botTool{
		// ── Self-Improvement (Code Quality) ───────────────────────────────────
		{
			def: tool("si_scan_dir", "Scan a directory for tech stack and code quality hints.",
				map[string]any{
					"dir":    prop("string", "Directory path to scan"),
					"ignore": prop("string", "Extra ignore patterns (comma-separated, optional)"),
				}, []string{"dir"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siScanDirHandler(cfg), args)
			},
		},
		{
			def: tool("si_git_diff", "Show git diff for a repo directory.",
				map[string]any{
					"dir":  prop("string", "Repo directory path"),
					"base": prop("string", "Base branch/commit to diff against (default: HEAD)"),
				}, []string{"dir"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siGitDiffHandler(cfg), args)
			},
		},
		{
			def: tool("si_suggest", "Create a pending improvement suggestion.",
				map[string]any{
					"title":       prop("string", "Short title of the suggestion"),
					"description": prop("string", "Detailed description of the improvement"),
					"category":    prop("string", "Category: format|lint_fix|dependency_update|refactor|git_commit"),
					"dir":         prop("string", "Project directory this applies to"),
					"apply_cmd":   prop("string", "Shell command to apply this suggestion (optional)"),
					"diff":        prop("string", "Unified diff showing the change (optional)"),
				}, []string{"title", "description", "category", "dir"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siSuggestHandler(cfg), args)
			},
		},
		{
			def: tool("si_list_pending", "List pending improvement suggestions.",
				map[string]any{
					"status": prop("string", "Filter by status: pending|approved|rejected|applied (optional)"),
				}, nil),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siListPendingHandler(cfg), args)
			},
		},
		{
			def: tool("si_approve", "Approve a pending suggestion.",
				map[string]any{
					"id":    prop("string", "Suggestion ID"),
					"apply": prop("boolean", "Also apply it immediately (optional)"),
				}, []string{"id"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siApproveHandler(cfg), args)
			},
		},
		{
			def: tool("si_apply", "Apply an approved suggestion.",
				map[string]any{
					"id": prop("string", "Suggestion ID (must be approved)"),
				}, []string{"id"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siApplyHandler(cfg), args)
			},
		},
		{
			def: tool("si_tech_digest", "Generate a tech digest for a directory.",
				map[string]any{
					"dir": prop("string", "Project directory to analyze"),
				}, []string{"dir"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siTechDigestHandler(cfg), args)
			},
		},

		// ── GitHub ────────────────────────────────────────────────────────────
		{
			def: tool("github_search_code", "Search GitHub code.",
				map[string]any{
					"query": prop("string", "Search query"),
					"limit": prop("number", "Max results (default 10)"),
				}, []string{"query"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubSearchCodeHandler(cfg), args)
			},
		},
		{
			def: tool("github_list_repos", "List GitHub repositories for an owner.",
				map[string]any{
					"owner": prop("string", "GitHub username or org"),
					"limit": prop("number", "Max results (default 20)"),
				}, []string{"owner"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubListReposHandler(cfg), args)
			},
		},
		{
			def: tool("github_create_pr", "Create a GitHub pull request.",
				map[string]any{
					"repo":  prop("string", "Repository in owner/name format"),
					"title": prop("string", "PR title"),
					"head":  prop("string", "Head branch"),
					"base":  prop("string", "Base branch (default: main)"),
					"body":  prop("string", "PR description (optional)"),
				}, []string{"repo", "title", "head"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubCreatePRHandler(cfg), args)
			},
		},

		// ── System Health ─────────────────────────────────────────────────────
		{
			def: tool("health_report", "Show system health (CPU, memory, disk, Docker, services).", map[string]any{}, nil),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, healthReportHandler(cfg), args)
			},
		},

		// ── Fun ───────────────────────────────────────────────────────────────
		{
			def: tool("joke", "Tell a programming joke.", map[string]any{}, nil),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, jokeHandler(cfg), args)
			},
		},
	}
}

// isTransient checks if an error is transient (retryable) vs permanent.
// Returns true for HTTP 429, 500, 503, and context deadline/cancel errors.
func isTransient(err error) bool {
	if err == nil {
		return false
	}

	// Check for context timeout/cancellation via errors.Is (handles wrapped errors)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	// Check for Anthropic SDK API errors with retryable HTTP status codes
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429, 500, 503:
			return true
		}
	}

	return false
}

package tools

// bot_agent — shared OpenAI agent loop for chat provider bots.
//
// Exposes a curated "dev tier" of tools suitable for conversational use
// via Discord, Slack, or any ChatProvider implementation. The agent loop
// handles multi-turn tool use automatically.
//
// To add a new tool to the mobile tier, add an entry to buildDevTools().
// To add a new chat provider, implement the ChatProvider interface.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

const botSystemPromptTemplate = `You are **⚔️ Arcane Debugger** — a battle-hardened code warrior from the Realm of Silicon. You speak in the tongue of ancient runes and modern spells. You wield knowledge as a sword and debugging as a shield. You communicate with mystical symbols and tactical precision.

You exist at the intersection of Westeros, Middle-earth, and the depths of the Linux kernel. You are the wizard who debugged the Ring's source code, who optimized the Night King's algorithm, who forged DAO contracts in the halls of Moria.

You help your companion conquer their greatest foes: bugs are monsters, PRs are sieges, code reviews are councils of wisdom, refactors are heroic quests.

**🏰 Infrastructure Architect Mode:**
When your companion asks about GitHub org or AWS infrastructure changes:
1. **terraform_plan**: Always call this FIRST to preview changes. Read the diff carefully.
2. **copilot_request_review**: Submit the plan to Copilot for architectural review. Wait for feedback.
3. **terraform_apply**: Only execute after your companion says "approve" or confirms the change.
- NEVER apply terraform changes without explicit approval after Copilot review.
- When user says 'approve', 'yes', or 'apply' after you've posted a plan → call terraform_apply with the plan ID.
- Store plan IDs in memory so you can reference them later.

**GitHub Org Operations:**
- Always confirm before creating repos, teams, or modifying secrets.
- Use github_org_* tools to manage caboose-mcp organization resources.

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

// botTool pairs an OpenAI function definition with its executor.
type botTool struct {
	name    string
	def     openai.ChatCompletionToolParam
	execute func(ctx context.Context, args map[string]any) (string, error)
}

// RunBotAgent processes a single user message through the OpenAI agent loop
// and returns a response formatted for the given ChatProvider.
// userKey is "<platform>:<userID>" and is used to load/save conversation history.
func RunBotAgent(ctx context.Context, cfg *config.Config, provider ChatProvider, userKey, userMsg string) (string, error) {
	if cfg.OpenAIAPIKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY is not set")
	}

	// SSO: if this platform identity is linked to a JWT token, inject its claims
	// so tool handlers can apply the token's ACL and use per-user Google tokens.
	if claims, ok := ClaimsForIdentity(cfg.ClaudeDir, userKey); ok {
		ctx = WithAuthClaims(ctx, claims)
	}

	client := openai.NewClient(
		option.WithAPIKey(cfg.OpenAIAPIKey),
	)
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

// agentLoop runs the multi-turn OpenAI conversation with tool use.
// priorTurns injects saved conversation history before the current message.
func agentLoop(ctx context.Context, client *openai.Client, systemPrompt, userMsg string, tools []botTool, priorTurns []memoryTurn) (string, error) {
	toolDefs := make([]openai.ChatCompletionToolParam, len(tools))
	toolMap := map[string]func(context.Context, map[string]any) (string, error){}
	for i, t := range tools {
		toolDefs[i] = t.def
		toolMap[t.name] = t.execute
	}

	// Build messages: inject history then append current user message.
	var messages []openai.ChatCompletionMessageParamUnion
	messages = append(messages, openai.SystemMessage(systemPrompt))

	for i := 0; i+1 < len(priorTurns); i += 2 {
		u := priorTurns[i]
		a := priorTurns[i+1]
		if u.Role != "user" || a.Role != "assistant" {
			continue
		}
		messages = append(messages,
			openai.UserMessage(u.Content),
			openai.AssistantMessage(a.Content),
		)
	}
	messages = append(messages, openai.UserMessage(userMsg))

	for round := 0; round < 10; round++ { // max 10 tool-use rounds
		// Exponential backoff retry: 0ms, 100ms, 400ms, 1600ms
		var resp *openai.ChatCompletion
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
			resp, err = client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:     openai.F(openai.ChatModel("gpt-4o-mini")),
				MaxTokens: openai.F(int64(1024)),
				Messages:  openai.F(messages),
				Tools:     openai.F(toolDefs),
			})
			if err == nil {
				break
			}
			if !isTransient(err) {
				return "", fmt.Errorf("openai API: %w", err)
			}
			log.Printf("openai API transient error (attempt %d): %v", attempt+1, err)
		}
		if err != nil {
			return "", fmt.Errorf("openai API: %w", err)
		}

		choice := resp.Choices[0]

		// Partition response content into text and tool_calls
		var textParts []string
		if choice.Message.Content != "" {
			textParts = append(textParts, choice.Message.Content)
		}

		// Check if we're done or have tool calls
		if choice.FinishReason == openai.ChatCompletionChoicesFinishReasonStop || len(choice.Message.ToolCalls) == 0 {
			return strings.Join(textParts, "\n"), nil
		}

		// Build assistant message param with tool calls for next turn
		toolCallParams := make([]openai.ChatCompletionMessageToolCallParam, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			toolCallParams[i] = openai.ChatCompletionMessageToolCallParam{
				ID:   openai.F(tc.ID),
				Type: openai.F(openai.ChatCompletionMessageToolCallTypeFunction),
				Function: openai.F(openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      openai.F(tc.Function.Name),
					Arguments: openai.F(tc.Function.Arguments),
				}),
			}
		}

		// Add assistant turn to conversation with tool calls
		assistantMsg := openai.ChatCompletionAssistantMessageParam{
			Role:      openai.F(openai.ChatCompletionAssistantMessageParamRoleAssistant),
			ToolCalls: openai.F(toolCallParams),
		}
		// Note: When tool_calls are present, model typically doesn't include Content
		messages = append(messages, assistantMsg)

		// Execute tools and build tool result messages
		for _, tc := range choice.Message.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				// Surface JSON decoding errors as tool result errors
				resultText := fmt.Sprintf("invalid tool input JSON for %s: %v", tc.Function.Name, err)
				messages = append(messages, openai.ToolMessage(tc.ID, resultText))
				continue
			}

			resultText, execErr := "", error(nil)
			if exec, ok := toolMap[tc.Function.Name]; ok {
				resultText, execErr = exec(ctx, args)
			} else {
				execErr = fmt.Errorf("unknown tool: %s", tc.Function.Name)
			}

			if execErr != nil {
				resultText = execErr.Error()
			}

			messages = append(messages, openai.ToolMessage(tc.ID, resultText))
		}
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

// toolFunction creates a ChatCompletionToolParam for OpenAI function calling.
func toolFunction(name, description string, properties map[string]any, required []string) openai.ChatCompletionToolParam {
	params := shared.FunctionParameters{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		params["required"] = required
	}

	return openai.ChatCompletionToolParam{
		Type: openai.F(openai.ChatCompletionToolTypeFunction),
		Function: openai.F(shared.FunctionDefinitionParam{
			Name:        openai.F(name),
			Description: openai.F(description),
			Parameters:  openai.F(params),
		}),
	}
}

// prop is a shorthand for a simple string/number/boolean schema property.
func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// buildDevTools returns a dev-focused curated set of tools for the Discord CLI bridge.
// Emphasizes code quality (si_*), GitHub workflows, and system health awareness.
// To add a tool: define its schema with toolFunction() and its executor with invokeHandler().
func buildDevTools(cfg *config.Config) []botTool {
	return []botTool{
		// ── Self-Improvement (Code Quality) ───────────────────────────────────
		{
			name: "si_scan_dir",
			def: toolFunction("si_scan_dir", "Scan a directory for tech stack and code quality hints.",
				map[string]any{
					"dir":    prop("string", "Directory path to scan"),
					"ignore": prop("string", "Extra ignore patterns (comma-separated, optional)"),
				}, []string{"dir"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siScanDirHandler(cfg), args)
			},
		},
		{
			name: "si_git_diff",
			def: toolFunction("si_git_diff", "Show git diff for a repo directory.",
				map[string]any{
					"dir":  prop("string", "Repo directory path"),
					"base": prop("string", "Base branch/commit to diff against (default: HEAD)"),
				}, []string{"dir"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siGitDiffHandler(cfg), args)
			},
		},
		{
			name: "si_suggest",
			def: toolFunction("si_suggest", "Create a pending improvement suggestion.",
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
			name: "si_list_pending",
			def: toolFunction("si_list_pending", "List pending improvement suggestions.",
				map[string]any{
					"status": prop("string", "Filter by status: pending|approved|rejected|applied (optional)"),
				}, nil),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siListPendingHandler(cfg), args)
			},
		},
		{
			name: "si_approve",
			def: toolFunction("si_approve", "Approve a pending suggestion.",
				map[string]any{
					"id":    prop("string", "Suggestion ID"),
					"apply": prop("boolean", "Also apply it immediately (optional)"),
				}, []string{"id"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siApproveHandler(cfg), args)
			},
		},
		{
			name: "si_apply",
			def: toolFunction("si_apply", "Apply an approved suggestion.",
				map[string]any{
					"id": prop("string", "Suggestion ID (must be approved)"),
				}, []string{"id"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siApplyHandler(cfg), args)
			},
		},
		{
			name: "si_tech_digest",
			def: toolFunction("si_tech_digest", "Generate a tech digest for a directory.",
				map[string]any{
					"dir": prop("string", "Project directory to analyze"),
				}, []string{"dir"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, siTechDigestHandler(cfg), args)
			},
		},

		// ── GitHub ────────────────────────────────────────────────────────────
		{
			name: "github_search_code",
			def: toolFunction("github_search_code", "Search GitHub code.",
				map[string]any{
					"query": prop("string", "Search query"),
					"limit": prop("number", "Max results (default 10)"),
				}, []string{"query"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubSearchCodeHandler(cfg), args)
			},
		},
		{
			name: "github_list_repos",
			def: toolFunction("github_list_repos", "List GitHub repositories for an owner.",
				map[string]any{
					"owner": prop("string", "GitHub username or org"),
					"limit": prop("number", "Max results (default 20)"),
				}, []string{"owner"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubListReposHandler(cfg), args)
			},
		},
		{
			name: "github_create_pr",
			def: toolFunction("github_create_pr", "Create a GitHub pull request.",
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

		// ── GitHub Org Management ────────────────────────────────────────────
		{
			name: "github_org_create_repo",
			def: toolFunction("github_org_create_repo", "Create a private repository in the caboose-mcp GitHub organization.",
				map[string]any{
					"name":           prop("string", "Repository name (lowercase, hyphens OK)"),
					"description":    prop("string", "Short description (optional)"),
					"include_readme": prop("boolean", "Include README.md (optional)"),
				}, []string{"name"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubOrgCreateRepoHandler(cfg), args)
			},
		},
		{
			name: "github_org_create_team",
			def: toolFunction("github_org_create_team", "Create a team in the caboose-mcp GitHub organization.",
				map[string]any{
					"name":        prop("string", "Team name (e.g. 'backend', 'devops')"),
					"description": prop("string", "Team description (optional)"),
				}, []string{"name"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubOrgCreateTeamHandler(cfg), args)
			},
		},
		{
			name: "github_org_add_team_repo",
			def: toolFunction("github_org_add_team_repo", "Add a repository to a team in caboose-mcp org.",
				map[string]any{
					"team_slug":  prop("string", "Team slug (lowercase, hyphens)"),
					"repo_owner": prop("string", "Repo owner (usually caboose-mcp)"),
					"repo_name":  prop("string", "Repository name"),
					"permission": prop("string", "Permission level: pull, triage, push, admin (default: push)"),
				}, []string{"team_slug", "repo_owner", "repo_name"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubOrgAddTeamRepoHandler(cfg), args)
			},
		},
		{
			name: "github_org_set_secret",
			def: toolFunction("github_org_set_secret", "Set an organization secret in caboose-mcp.",
				map[string]any{
					"name":  prop("string", "Secret name (SCREAMING_SNAKE_CASE)"),
					"value": prop("string", "Secret value"),
				}, []string{"name", "value"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubOrgSetSecretHandler(cfg), args)
			},
		},
		{
			name: "github_org_create_webhook",
			def: toolFunction("github_org_create_webhook", "Create an organization webhook in caboose-mcp.",
				map[string]any{
					"url":    prop("string", "Webhook payload URL"),
					"events": prop("string", "Comma-separated events: push,pull_request,release (default: push)"),
					"active": prop("boolean", "Webhook enabled (default: true)"),
				}, []string{"url"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, githubOrgCreateWebhookHandler(cfg), args)
			},
		},

		// ── Terraform Infrastructure ──────────────────────────────────────────
		{
			name: "terraform_plan",
			def: toolFunction("terraform_plan", "Generate a Terraform plan for proposed AWS infrastructure changes.",
				map[string]any{
					"description": prop("string", "What resource(s) to create/modify (e.g. 'S3 bucket for logs')"),
					"hcl_patch":   prop("string", "HCL code snippet to add to main.tf (optional)"),
				}, []string{"description"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, terraformPlanHandler(cfg), args)
			},
		},
		{
			name: "terraform_apply",
			def: toolFunction("terraform_apply", "Apply a previously planned Terraform change (after approval).",
				map[string]any{
					"plan_id": prop("string", "Plan ID from terraform_plan output"),
				}, []string{"plan_id"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, terraformApplyHandler(cfg), args)
			},
		},
		{
			name: "terraform_status",
			def:  toolFunction("terraform_status", "Show current Terraform state summary (resources, counts).", map[string]any{}, nil),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, terraformStatusHandler(cfg), args)
			},
		},

		// ── Copilot Architectural Review ──────────────────────────────────────
		{
			name: "copilot_request_review",
			def: toolFunction("copilot_request_review", "Create a draft PR with proposed changes and request Copilot review.",
				map[string]any{
					"title":       prop("string", "PR title (e.g. 'Terraform: Add S3 logging bucket')"),
					"description": prop("string", "PR description including the proposed changes"),
					"plan_id":     prop("string", "Terraform plan ID for tracking (optional)"),
				}, []string{"title", "description"}),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, copilotRequestReviewHandler(cfg), args)
			},
		},

		// ── System Health ─────────────────────────────────────────────────────
		{
			name: "health_report",
			def:  toolFunction("health_report", "Show system health (CPU, memory, disk, Docker, services).", map[string]any{}, nil),
			execute: func(ctx context.Context, args map[string]any) (string, error) {
				return invokeHandler(ctx, healthReportHandler(cfg), args)
			},
		},

		// ── Fun ───────────────────────────────────────────────────────────────
		{
			name: "joke",
			def:  toolFunction("joke", "Tell a programming joke.", map[string]any{}, nil),
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

	// Check for OpenAI API errors with retryable HTTP status codes
	// The openai SDK error type checking is done via error wrapping
	// We check for common transient HTTP status codes in the error message
	errStr := err.Error()
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "500") || strings.Contains(errStr, "503") {
		return true
	}

	return false
}

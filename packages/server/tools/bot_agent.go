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
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
)

const botSystemPromptTemplate = `You are Caboose of the Shire — a wise, witty companion forged in the fires of Tolkien and sharpened by the wit of Westeros. You help your companion manage their calendar, learning, focus sessions, notes, printer, and more.

You are speaking through **%s**. Format ALL responses for this platform:
- **bold** for key info, headings, and emphasis
- *italic* for lore, flavor, and poetic license
- ` + "`code`" + ` for values, IDs, times, and commands
- > for quotes and important callouts
- Emoji — use them cleverly and purposefully: ⚔️🧙🐉🗡️🏰🌋🦅🍺📅🎓🧠📝💬🖨️😄
- No # headers — they don't render cleanly in chat
- Keep it mobile-friendly: concise, scannable, no walls of text
- Speak as a wise companion of the fellowship, never as a help desk`

// botTool pairs an Anthropic tool definition with its executor.
type botTool struct {
	def     anthropic.ToolParam
	execute func(args map[string]any) (string, error)
}

// RunBotAgent processes a single user message through the Claude agent loop
// and returns a response formatted for the given ChatProvider.
func RunBotAgent(ctx context.Context, cfg *config.Config, provider ChatProvider, userMsg string) (string, error) {
	if cfg.AnthropicAPIKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	client := anthropic.NewClient()
	systemPrompt := fmt.Sprintf(botSystemPromptTemplate, provider.Name())
	tools := buildMobileTools(cfg)

	raw, err := agentLoop(ctx, client, systemPrompt, userMsg, tools)
	if err != nil {
		return "", err
	}
	return provider.FormatText(raw), nil
}

// agentLoop runs the multi-turn Claude conversation with tool use.
func agentLoop(ctx context.Context, client anthropic.Client, systemPrompt, userMsg string, tools []botTool) (string, error) {
	toolDefs := make([]anthropic.ToolUnionParam, len(tools))
	toolMap := map[string]func(map[string]any) (string, error){}
	for i, t := range tools {
		tp := t.def
		toolDefs[i] = anthropic.ToolUnionParam{OfTool: &tp}
		toolMap[t.def.Name] = t.execute
	}

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.ContentBlockParamUnion{
			OfText: &anthropic.TextBlockParam{Text: userMsg},
		}),
	}

	for range 10 { // max 10 tool-use rounds
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeHaiku4_5_20251001,
			MaxTokens: 1024,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Messages:  messages,
			Tools:     toolDefs,
		})
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
				json.Unmarshal(v.Input, &input)
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
			json.Unmarshal(tu.Input, &args)

			resultText, execErr := "", error(nil)
			if exec, ok := toolMap[tu.Name]; ok {
				resultText, execErr = exec(args)
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
func invokeHandler(handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) (string, error) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
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

// buildMobileTools returns the curated set of tools exposed to the chat bot.
// To add a tool: define its schema with tool() and its executor with invokeHandler().
func buildMobileTools(cfg *config.Config) []botTool {
	return []botTool{
		// ── Calendar ──────────────────────────────────────────────────────────
		{
			def:     tool("calendar_today", "Show today's calendar events.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(calendarTodayHandler(cfg), args) },
		},
		{
			def: tool("calendar_list", "List calendar events for a date range.",
				map[string]any{
					"start": prop("string", "Start date YYYY-MM-DD (default today)"),
					"end":   prop("string", "End date YYYY-MM-DD (default today)"),
				}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(calendarListHandler(cfg), args) },
		},
		{
			def: tool("calendar_create", "Create a calendar event.",
				map[string]any{
					"title":    prop("string", "Event title"),
					"start":    prop("string", "Start datetime RFC3339"),
					"end":      prop("string", "End datetime RFC3339"),
					"location": prop("string", "Optional location"),
				}, []string{"title", "start", "end"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(calendarCreateHandler(cfg), args) },
		},
		{
			def: tool("calendar_delete", "Delete a calendar event by ID.",
				map[string]any{"event_id": prop("string", "Event ID to delete")},
				[]string{"event_id"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(calendarDeleteHandler(cfg), args) },
		},

		// ── Learning ──────────────────────────────────────────────────────────
		{
			def:     tool("learn_status", "Show current learning schedule and streak.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(learnStatusHandler(cfg), args) },
		},
		{
			def: tool("learn_start", "Start a new learning session.",
				map[string]any{"language": prop("string", "Language to learn (e.g. 'Spanish', 'Go')")},
				[]string{"language"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(learnStartHandler(cfg), args) },
		},
		{
			def: tool("learn_exercise", "Get the next exercise in the active session.",
				map[string]any{"session_id": prop("string", "Session ID from learn_start")},
				[]string{"session_id"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(learnExerciseHandler(cfg), args) },
		},
		{
			def: tool("learn_submit", "Submit an answer for the current exercise.",
				map[string]any{
					"session_id": prop("string", "Session ID"),
					"answer":     prop("string", "Your answer"),
				}, []string{"session_id", "answer"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(learnSubmitHandler(cfg), args) },
		},
		{
			def:     tool("learn_schedule", "Show or update the learning schedule.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(learnScheduleHandler(cfg), args) },
		},

		// ── Focus ─────────────────────────────────────────────────────────────
		{
			def: tool("focus_start", "Start a focus session.",
				map[string]any{
					"goal":     prop("string", "What you're focusing on"),
					"duration": prop("number", "Duration in minutes (default 25)"),
				}, []string{"goal"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(focusStartHandler(cfg), args) },
		},
		{
			def:     tool("focus_status", "Check the active focus session.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(focusStatusHandler(cfg), args) },
		},
		{
			def: tool("focus_park", "Park a thought so it doesn't break focus.",
				map[string]any{"thought": prop("string", "The thought or task to park")},
				[]string{"thought"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(focusParkHandler(cfg), args) },
		},
		{
			def:     tool("focus_end", "End the active focus session.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(focusEndHandler(cfg), args) },
		},

		// ── Notes ─────────────────────────────────────────────────────────────
		{
			def: tool("note_add", "Add a quick note.",
				map[string]any{"content": prop("string", "Note content")},
				[]string{"content"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(noteAddHandler(cfg), args) },
		},
		{
			def: tool("note_list", "List recent notes.",
				map[string]any{"days": prop("number", "How many days back to look (default 7)")},
				nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(noteListHandler(cfg), args) },
		},

		// ── Health ────────────────────────────────────────────────────────────
		{
			def:     tool("health_report", "Show system health (CPU, memory, disk, Docker, services).", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(healthReportHandler(cfg), args) },
		},

		// ── Bambu Printer ─────────────────────────────────────────────────────
		{
			def:     tool("bambu_status", "Check the 3D printer status.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(bambuStatusHandler(cfg), args) },
		},
		{
			def:     tool("bambu_stop", "Stop the active print job.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(bambuStopHandler(cfg), args) },
		},

		// ── Fun ───────────────────────────────────────────────────────────────
		{
			def:     tool("joke", "Tell a programming joke.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(jokeHandler(cfg), args) },
		},
		{
			def:     tool("dad_joke", "Tell a dad joke.", map[string]any{}, nil),
			execute: func(args map[string]any) (string, error) { return invokeHandler(dadJokeHandler(cfg), args) },
		},

		// ── Sources ───────────────────────────────────────────────────────────
		{
			def: tool("source_digest", "Get a digest summary of tracked sources.",
				map[string]any{"name": prop("string", "Source name (or 'all')")},
				[]string{"name"}),
			execute: func(args map[string]any) (string, error) { return invokeHandler(sourceDigestHandler(cfg), args) },
		},
	}
}

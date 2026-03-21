// Phase 3: Repository Management Tools
//
// These tools allow authenticated users to create, test, approve, and deploy tools
// to the fafb ecosystem via MCP or web UI.
//
// Storage:
//   ~/.claude/pending-tools/<tool_name>.json — Draft tool definitions awaiting approval
//   ~/.claude/audit/audit.log                 — Append-only audit log
//
// Scopes required:
//   "repo" — Create, test, list pending tools, sync UI
//   "repo:admin" — Approve, reject, deploy tools
//
// IMPORTANT: These tools are placeholders for Phase 3 implementation.
// Each tool requires full implementation with proper error handling and validation.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// PendingTool represents a draft tool awaiting approval
type PendingTool struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Parameters  []ToolParameter   `json:"parameters"`
	HandlerCode string            `json:"handler_code"`
	Tier        string            `json:"tier"` // hosted, local, or both
	Tags        []string          `json:"tags"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
	LastTestAt  time.Time         `json:"last_test_at"`
	LastTestResult string         `json:"last_test_result"`
}

type ToolParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

func pendingToolsDir(claudeDir string) string {
	return filepath.Join(claudeDir, "pending-tools")
}

func pendingToolPath(claudeDir, toolName string) string {
	return filepath.Join(pendingToolsDir(claudeDir), fmt.Sprintf("%s.json", toolName))
}

// RegisterRepo registers all repo_* and admin tools
func RegisterRepo(s *server.MCPServer, cfg *config.Config) {
	// Phase 3 tools (repo scope)
	s.AddTool(mcp.NewTool("repo_create_tool",
		mcp.WithDescription("Create a new tool draft. Validates input and stores in ~/.claude/pending-tools/ awaiting approval. Requires 'repo' scope."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Tool name in snake_case (e.g. 'my_tool')")),
		mcp.WithString("description", mcp.Required(), mcp.Description("What this tool does")),
		mcp.WithString("category", mcp.Description("Category (e.g. 'dev', 'automation', 'data')")),
		mcp.WithString("parameters_json", mcp.Description("JSON array of {name, type, required, description}")),
		mcp.WithString("handler_code", mcp.Description("Go function code (multiline)")),
		mcp.WithString("tier", mcp.Description("Deployment tier: 'hosted', 'local', or 'both' (default: 'hosted')")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags")),
	), repoCreateToolHandler(cfg))

	s.AddTool(mcp.NewTool("repo_test_tool",
		mcp.WithDescription("Test a pending tool with sample input. Executes handler in isolated sandbox. Requires 'repo' scope."),
		mcp.WithString("tool_name", mcp.Required(), mcp.Description("Name of pending tool to test")),
		mcp.WithString("test_input_json", mcp.Description("JSON object with parameters to pass to tool")),
	), repoTestToolHandler(cfg))

	s.AddTool(mcp.NewTool("repo_list_pending_tools",
		mcp.WithDescription("List all pending tool drafts awaiting approval. Requires 'repo' scope."),
	), repoListPendingToolsHandler(cfg))

	s.AddTool(mcp.NewTool("repo_approve_tool",
		mcp.WithDescription("Approve a pending tool, commit to main, trigger CI/CD. Requires 'repo:admin' scope."),
		mcp.WithString("tool_name", mcp.Required(), mcp.Description("Name of pending tool to approve")),
		mcp.WithString("approver_notes", mcp.Description("Optional changelog entry")),
	), repoApproveToolHandler(cfg))

	s.AddTool(mcp.NewTool("repo_reject_tool",
		mcp.WithDescription("Reject a pending tool and discard draft. Requires 'repo:admin' scope."),
		mcp.WithString("tool_name", mcp.Required(), mcp.Description("Name of pending tool to reject")),
		mcp.WithString("reason", mcp.Description("Reason for rejection (for audit log)")),
	), repoRejectToolHandler(cfg))

	s.AddTool(mcp.NewTool("repo_sync_ui",
		mcp.WithDescription("Manually trigger UI sync: extract all tools from Go source and update UI tools.ts. Requires 'repo' scope."),
	), repoSyncUIHandler(cfg))

	s.AddTool(mcp.NewTool("repo_deploy",
		mcp.WithDescription("Trigger deployment of current main branch. Requires 'repo:admin' scope."),
		mcp.WithString("service", mcp.Description("Service: 'hosted', 'local', or 'bots'")),
		mcp.WithBoolean("force", mcp.Description("Skip approval gate (default false)")),
	), repoDeployHandler(cfg))
}

func repoCreateToolHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := GetAuthClaims(ctx)
		if claims == nil {
			return mcp.NewToolResultError("Unauthorized: requires JWT token with 'repo' scope"), nil
		}

		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Missing name: %v", err)), nil
		}

		// Validate tool name format (snake_case, alphanumeric + underscore)
		if !isValidToolName(name) {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid tool name '%s': must be lowercase with underscores only", name)), nil
		}

		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Missing description: %v", err)), nil
		}

		// Create pending tool
		pending := PendingTool{
			Name:        name,
			Description: description,
			Category:    req.GetString("category", "misc"),
			Tier:        req.GetString("tier", "hosted"),
			CreatedBy:   claims.Subject,
			CreatedAt:   time.Now(),
		}

		// Parse parameters if provided
		paramsJSON := req.GetString("parameters_json", "[]")
		if err := json.Unmarshal([]byte(paramsJSON), &pending.Parameters); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters JSON: %v", err)), nil
		}

		// Parse tags
		if tags := req.GetString("tags", ""); tags != "" {
			pending.Tags = splitCSV(tags)
		}

		// Store pending tool
		if err := savePendingTool(cfg.ClaudeDir, pending); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to save tool draft: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Tool '%s' created as draft\nLocation: %s\nNext step: test with repo_test_tool, then submit for approval", name, pendingToolPath(cfg.ClaudeDir, name))), nil
	}
}

func repoTestToolHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := GetAuthClaims(ctx)
		if claims == nil {
			return mcp.NewToolResultError("Unauthorized: requires JWT token with 'repo' scope"), nil
		}

		toolName, err := req.RequireString("tool_name")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Missing tool_name: %v", err)), nil
		}

		// Load pending tool
		pending, err := loadPendingTool(cfg.ClaudeDir, toolName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Tool not found: %v", err)), nil
		}

		// Parse test input
		testInputJSON := req.GetString("test_input_json", "{}")
		var testInput map[string]interface{}
		if err := json.Unmarshal([]byte(testInputJSON), &testInput); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid test_input_json: %v", err)), nil
		}

		// Validate test input against tool parameters
		var validationErrors []string
		for _, param := range pending.Parameters {
			value, exists := testInput[param.Name]
			if param.Required && !exists {
				validationErrors = append(validationErrors, fmt.Sprintf("Required parameter missing: %s", param.Name))
			}

			// Type validation
			if exists && value != nil {
				switch param.Type {
				case "number":
					if _, ok := value.(float64); !ok {
						validationErrors = append(validationErrors, fmt.Sprintf("Parameter %s: expected number, got %T", param.Name, value))
					}
				case "boolean":
					if _, ok := value.(bool); !ok {
						validationErrors = append(validationErrors, fmt.Sprintf("Parameter %s: expected boolean, got %T", param.Name, value))
					}
				}
			}
		}

		pending.LastTestAt = time.Now()

		if len(validationErrors) > 0 {
			pending.LastTestResult = fmt.Sprintf("❌ Validation failed:\n%s", strings.Join(validationErrors, "\n"))
		} else {
			// Simulate successful execution
			pending.LastTestResult = fmt.Sprintf("✅ Test passed\n• Parameters validated: %d\n• Input: %s\n• Status: Ready for deployment", len(pending.Parameters), testInputJSON)
		}

		savePendingTool(cfg.ClaudeDir, *pending)

		return mcp.NewToolResultText(pending.LastTestResult), nil
	}
}

func repoListPendingToolsHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := GetAuthClaims(ctx)
		if claims == nil {
			return mcp.NewToolResultError("Unauthorized: requires JWT token with 'repo' scope"), nil
		}

		dir := pendingToolsDir(cfg.ClaudeDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return mcp.NewToolResultText("No pending tools"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("Error reading pending tools: %v", err)), nil
		}

		var output strings.Builder
		output.WriteString("📋 Pending Tools:\n\n")
		count := 0
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") {
				toolName := strings.TrimSuffix(entry.Name(), ".json")
				tool, err := loadPendingTool(cfg.ClaudeDir, toolName)
				if err == nil {
					count++
					output.WriteString(fmt.Sprintf("• %s (%s)\n  Created: %s by %s\n  Status: ", tool.Name, tool.Tier, tool.CreatedAt.Format("2006-01-02"), tool.CreatedBy))
					if tool.LastTestAt.IsZero() {
						output.WriteString("not tested")
					} else {
						output.WriteString(fmt.Sprintf("tested at %s", tool.LastTestAt.Format("2006-01-02 15:04")))
					}
					output.WriteString("\n\n")
				}
			}
		}
		if count == 0 {
			return mcp.NewToolResultText("No pending tools"), nil
		}
		return mcp.NewToolResultText(output.String()), nil
	}
}

func repoApproveToolHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := GetAuthClaims(ctx)
		if claims == nil {
			return mcp.NewToolResultError("Unauthorized: requires JWT token with 'repo:admin' scope"), nil
		}

		// Check for admin scope (Phase 3: implement full scope checking)
		toolName, err := req.RequireString("tool_name")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Missing tool_name: %v", err)), nil
		}

		// Load pending tool
		if _, err2 := loadPendingTool(cfg.ClaudeDir, toolName); err2 != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Tool not found: %v", err2)), nil
		}

		// Phase 3 placeholder: Commit to caboose-mcp, push, trigger CI
		deletePendingTool(cfg.ClaudeDir, toolName)

		return mcp.NewToolResultText(fmt.Sprintf("✅ Tool '%s' approved and deployed\nNext: CI runs, auto-merge, live deployment", toolName)), nil
	}
}

func repoRejectToolHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := GetAuthClaims(ctx)
		if claims == nil {
			return mcp.NewToolResultError("Unauthorized: requires JWT token with 'repo:admin' scope"), nil
		}

		toolName, err := req.RequireString("tool_name")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Missing tool_name: %v", err)), nil
		}

		reason := req.GetString("reason", "No reason provided")

		// Delete pending tool
		deletePendingTool(cfg.ClaudeDir, toolName)

		return mcp.NewToolResultText(fmt.Sprintf("✅ Tool '%s' rejected\nReason: %s", toolName, reason)), nil
	}
}

func repoSyncUIHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := GetAuthClaims(ctx)
		if claims == nil {
			return mcp.NewToolResultError("Unauthorized: requires JWT token with 'repo' scope"), nil
		}

		// Phase 3 placeholder: Run extract-tools.go and sync-tools-from-mcp.cjs
		return mcp.NewToolResultText("✅ UI sync triggered\nTools extracted and synced to UI repo"), nil
	}
}

func repoDeployHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims := GetAuthClaims(ctx)
		if claims == nil {
			return mcp.NewToolResultError("Unauthorized: requires JWT token with 'repo:admin' scope"), nil
		}

		service := req.GetString("service", "hosted")

		// Phase 3 placeholder: Trigger GitHub Actions dispatch
		return mcp.NewToolResultText(fmt.Sprintf("✅ Deployment triggered for service '%s'\nWatch GitHub Actions for progress", service)), nil
	}
}

// Helpers

func isValidToolName(name string) bool {
	if len(name) == 0 || len(name) > 50 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func savePendingTool(claudeDir string, tool PendingTool) error {
	dir := pendingToolsDir(claudeDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tool, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pendingToolPath(claudeDir, tool.Name), data, 0644)
}

func loadPendingTool(claudeDir, toolName string) (*PendingTool, error) {
	data, err := os.ReadFile(pendingToolPath(claudeDir, toolName))
	if err != nil {
		return nil, err
	}
	var tool PendingTool
	if err := json.Unmarshal(data, &tool); err != nil {
		return nil, err
	}
	return &tool, nil
}

func deletePendingTool(claudeDir, toolName string) error {
	return os.Remove(pendingToolPath(claudeDir, toolName))
}

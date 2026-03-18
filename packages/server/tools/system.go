package tools

// system — shell command execution with optional audit gating.
//
// execute_command runs arbitrary shell commands via `sh -c`. It routes through
// GateOrRun so that when the audit gate is enabled and execute_command is on
// the gate list, execution is deferred until explicitly approved via
// approve_execution. All invocations are logged to the audit trail.
//
// Tools:
//   execute_command — run a shell command and return combined stdout+stderr

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterSystem(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("execute_command",
		mcp.WithDescription("Execute a shell command and return stdout+stderr."),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to run")),
		mcp.WithString("cwd", mcp.Description("Working directory for the command")),
	), executeCommandHandler(cfg))
}

func executeCommandHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		command, err := req.RequireString("command")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		cwd := req.GetString("cwd", "")
		params := map[string]string{"command": command}
		if cwd != "" {
			params["cwd"] = cwd
		}
		return GateOrRun(cfg, "execute_command", params, func() (string, error) {
			cmd := exec.Command("sh", "-c", command)
			if cwd != "" {
				cmd.Dir = cwd
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("exit error: %v\n%s", err, out)
			}
			return string(out), nil
		})
	}
}

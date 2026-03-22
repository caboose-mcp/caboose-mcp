package tools

// env — environment check and remediation.
//
// Tools:
//   env_check — check whether common dev tools are installed, show versions
//   env_fix   — install one or all missing tools (gated by audit)

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// envTool describes a tool to check / how to install it.
type envTool struct {
	name    string // binary name
	label   string // human label
	aptCmd  string // apt-get install arg
	brewCmd string // brew install arg
	altCmd  string // alternative install command (e.g. pip/cargo)
}

var envTools = []envTool{
	{name: "node", label: "Node.js"},
	{name: "npm", label: "npm"},
	{name: "pnpm", label: "pnpm", altCmd: "npm install -g pnpm"},
	{name: "go", label: "Go"},
	{name: "python3", label: "Python", aptCmd: "python3", brewCmd: "python"},
	{name: "pip3", label: "pip", aptCmd: "python3-pip", brewCmd: "python", altCmd: "python3 -m ensurepip --upgrade"},
	{name: "uv", label: "uv (Python)", altCmd: "pip install uv"},
	{name: "docker", label: "Docker"},
	{name: "git", label: "git", aptCmd: "git", brewCmd: "git"},
	{name: "gh", label: "GitHub CLI", aptCmd: "gh", brewCmd: "gh"},
	{name: "aws", label: "AWS CLI"},
	{name: "terraform", label: "Terraform"},
	{name: "cargo", label: "Rust/cargo"},
	{name: "make", label: "make", aptCmd: "build-essential", brewCmd: "make"},
	{name: "jq", label: "jq", aptCmd: "jq", brewCmd: "jq"},
	{name: "curl", label: "curl", aptCmd: "curl", brewCmd: "curl"},
	{name: "wget", label: "wget", aptCmd: "wget", brewCmd: "wget"},
	{name: "sqlite3", label: "sqlite3", aptCmd: "sqlite3", brewCmd: "sqlite"},
	{name: "psql", label: "PostgreSQL client", aptCmd: "postgresql-client", brewCmd: "postgresql"},
	{name: "pre-commit", label: "pre-commit", aptCmd: "pre-commit", brewCmd: "pre-commit", altCmd: "pip install pre-commit"},
	{name: "code", label: "VS Code"},
}

func RegisterEnv(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("env_check",
		mcp.WithDescription("Check which dev tools are installed and show their versions. "+
			"Returns a formatted report with ✅ installed / ❌ missing status and install commands for missing tools."),
		mcp.WithBoolean("missing_only", mcp.Description("Only list missing tools (default: false)")),
	), envCheckHandler(cfg))

	s.AddTool(mcp.NewTool("env_fix",
		mcp.WithDescription("Install one or all missing dev tools. "+
			"Runs the appropriate install command (apt-get / pip / npm). "+
			"Gated by the audit system when gate mode is enabled."),
		mcp.WithString("tool", mcp.Description("Tool name to install (e.g. 'pre-commit'). Omit to install all missing.")),
		mcp.WithString("method", mcp.Description("Install method: 'apt', 'brew', or 'alt'. Defaults to 'apt'.")),
	), envFixHandler(cfg))
}

func envCheckHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		missingOnly := req.GetBool("missing_only", false)

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# Environment Check\n\n"))

		var installed, missing []string

		for _, t := range envTools {
			ver := toolVersion(t.name)
			if ver != "" {
				if !missingOnly {
					installed = append(installed, fmt.Sprintf("✅  %-16s %s", t.name, ver))
				}
			} else {
				line := fmt.Sprintf("❌  %-16s %s", t.name, t.label)
				if t.aptCmd != "" {
					line += fmt.Sprintf("\n    apt-get: sudo apt-get install -y %s", t.aptCmd)
				}
				if t.brewCmd != "" {
					line += fmt.Sprintf("\n    brew:    brew install %s", t.brewCmd)
				}
				if t.altCmd != "" {
					line += fmt.Sprintf("\n    alt:     %s", t.altCmd)
				}
				line += "\n    fix:     env_fix tool=" + t.name
				missing = append(missing, line)
			}
		}

		if !missingOnly && len(installed) > 0 {
			sb.WriteString(fmt.Sprintf("## Installed (%d)\n", len(installed)))
			for _, l := range installed {
				sb.WriteString(l + "\n")
			}
			sb.WriteString("\n")
		}

		if len(missing) > 0 {
			sb.WriteString(fmt.Sprintf("## Missing (%d)\n", len(missing)))
			for _, l := range missing {
				sb.WriteString(l + "\n")
			}
		} else {
			sb.WriteString("## All tools installed ✅\n")
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func envFixHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		toolName := req.GetString("tool", "")
		method := req.GetString("method", "apt")

		var toFix []envTool

		if toolName != "" {
			for _, t := range envTools {
				if t.name == toolName {
					toFix = append(toFix, t)
					break
				}
			}
			if len(toFix) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("unknown tool: %q", toolName)), nil
			}
		} else {
			// install all missing
			for _, t := range envTools {
				if toolVersion(t.name) == "" {
					toFix = append(toFix, t)
				}
			}
			if len(toFix) == 0 {
				return mcp.NewToolResultText("All tools already installed."), nil
			}
		}

		var sb strings.Builder
		for _, t := range toFix {
			cmd := installCmd(t, method)
			if cmd == "" {
				sb.WriteString(fmt.Sprintf("⚠️  %s: no install command for method=%q\n", t.name, method))
				continue
			}
			sb.WriteString(fmt.Sprintf("→ Installing %s: %s\n", t.name, cmd))
			res, err := GateOrRun(cfg, "env_fix", map[string]string{"tool": t.name, "cmd": cmd}, func() (string, error) {
				cmdOut, runErr := exec.Command("sh", "-c", cmd).CombinedOutput()
				return string(cmdOut), runErr
			})
			if res != nil {
				for _, c := range res.Content {
					if tc, ok := c.(mcp.TextContent); ok {
						sb.WriteString(tc.Text + "\n")
					}
				}
			}
			if err != nil {
				sb.WriteString(fmt.Sprintf("  error: %v\n", err))
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

// toolVersion returns the version string for a binary, or "" if not found.
func toolVersion(name string) string {
	// try --version first, then version
	for _, flag := range []string{"--version", "version"} {
		out, err := exec.Command(name, flag).CombinedOutput()
		if err == nil && len(out) > 0 {
			line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
			if len(line) > 80 {
				line = line[:80]
			}
			return line
		}
	}
	return ""
}

func installCmd(t envTool, method string) string {
	switch method {
	case "brew":
		if t.brewCmd != "" {
			return "brew install " + t.brewCmd
		}
	case "alt":
		if t.altCmd != "" {
			return t.altCmd
		}
	default: // apt
		if t.aptCmd != "" {
			return "sudo apt-get install -y " + t.aptCmd
		}
		if t.altCmd != "" {
			return t.altCmd
		}
	}
	return ""
}

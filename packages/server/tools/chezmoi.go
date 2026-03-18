package tools

// chezmoi — dotfile manager tools.
//
// All tools are thin wrappers around the chezmoi CLI.
// chezmoi must be installed: https://www.chezmoi.io/install/
//
// Tools:
//   chezmoi_status       show managed files and their diff state
//   chezmoi_diff         preview what `apply` would change
//   chezmoi_apply        apply source state to the home directory
//   chezmoi_add          add a file/dir to chezmoi management
//   chezmoi_forget       stop managing a file (remove from source, keep target)
//   chezmoi_update       pull latest from source repo and apply
//   chezmoi_init         initialise chezmoi (optionally from a repo URL)
//   chezmoi_data         show template variables (useful for debugging templates)
//   chezmoi_managed      list all managed files
//   chezmoi_git          run an arbitrary git command in the chezmoi source dir
//
// Note: `amauryconstant/chezmoi-mcp` is a separate TypeScript/Bun MCP server
// with 25+ tools and structured parsers. If you need full chezmoi coverage,
// add it alongside caboose-mcp in .mcp.json (see setup_github_mcp_info for
// the multi-server pattern). These native tools cover the core daily workflow
// without requiring Bun.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterChezmoi(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("chezmoi_status",
		mcp.WithDescription("Show chezmoi status: which managed files differ from their source state."),
		mcp.WithString("path", mcp.Description("Limit to a specific file or directory (optional)")),
	), chezmoiStatusHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_diff",
		mcp.WithDescription("Preview changes chezmoi would make to the home directory without applying them."),
		mcp.WithString("path", mcp.Description("Limit diff to a specific file or directory (optional)")),
	), chezmoiDiffHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_apply",
		mcp.WithDescription("Apply chezmoi source state to the home directory."),
		mcp.WithString("path", mcp.Description("Apply only a specific file or directory (optional)")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview what would be applied without making changes (default false)")),
		mcp.WithBoolean("verbose", mcp.Description("Show each change as it is applied")),
	), chezmoiApplyHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_add",
		mcp.WithDescription("Add a file or directory to chezmoi management (copies it into the source state)."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or ~ path to the file/directory to add")),
		mcp.WithBoolean("template", mcp.Description("Treat as a template (adds .tmpl extension)")),
		mcp.WithBoolean("encrypt", mcp.Description("Encrypt the file in the source state")),
		mcp.WithBoolean("exact", mcp.Description("Add as exact (only listed files will be present in target)")),
	), chezmoiAddHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_forget",
		mcp.WithDescription("Stop managing a file with chezmoi. Removes it from the source state but leaves the target file intact."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to forget (target or source path)")),
	), chezmoiForgetHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_update",
		mcp.WithDescription("Pull the latest changes from the chezmoi source repo and apply them."),
		mcp.WithBoolean("dry_run", mcp.Description("Preview changes without applying")),
	), chezmoiUpdateHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_init",
		mcp.WithDescription("Initialise chezmoi, optionally cloning dotfiles from a git repo."),
		mcp.WithString("repo", mcp.Description("Git repo URL or GitHub username (e.g. 'caboose' clones github.com/caboose/dotfiles). Leave empty to init without a repo.")),
		mcp.WithBoolean("apply", mcp.Description("Apply the source state immediately after init (default false)")),
		mcp.WithBoolean("purge", mcp.Description("Remove existing source dir before init")),
	), chezmoiInitHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_data",
		mcp.WithDescription("Show all chezmoi template data variables (hostname, username, os, custom data, etc.)."),
		mcp.WithString("format", mcp.Description("Output format: json (default) or yaml")),
	), chezmoiDataHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_managed",
		mcp.WithDescription("List all files managed by chezmoi."),
		mcp.WithString("path_style", mcp.Description("Path style: absolute (default), relative, source-absolute, source-relative")),
		mcp.WithString("include", mcp.Description("Comma-separated types to include: files,dirs,symlinks,encrypted (default: files)")),
	), chezmoiManagedHandler(cfg))

	s.AddTool(mcp.NewTool("chezmoi_git",
		mcp.WithDescription("Run a git command inside the chezmoi source directory (e.g. to commit dotfile changes or check remote status)."),
		mcp.WithString("args", mcp.Required(), mcp.Description("Git arguments, e.g. 'status', 'log --oneline -10', 'push'")),
	), chezmoiGitHandler(cfg))
}

// ---- helpers ----

// chezmoiRun executes chezmoi with the given arguments and returns combined output.
func chezmoiRun(args ...string) (string, error) {
	out, err := exec.Command("chezmoi", args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("%s", text)
		}
		return "", err
	}
	if text == "" {
		return "(no output)", nil
	}
	return text, nil
}

// ---- handlers ----

func chezmoiStatusHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := []string{"status"}
		if path := req.GetString("path", ""); path != "" {
			args = append(args, path)
		}
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiDiffHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := []string{"diff"}
		if path := req.GetString("path", ""); path != "" {
			args = append(args, path)
		}
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiApplyHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := []string{"apply"}
		if req.GetBool("dry_run", false) {
			args = append(args, "--dry-run")
		}
		if req.GetBool("verbose", false) {
			args = append(args, "--verbose")
		}
		if path := req.GetString("path", ""); path != "" {
			args = append(args, path)
		}
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiAddHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		args := []string{"add"}
		if req.GetBool("template", false) {
			args = append(args, "--template")
		}
		if req.GetBool("encrypt", false) {
			args = append(args, "--encrypt")
		}
		if req.GetBool("exact", false) {
			args = append(args, "--exact")
		}
		args = append(args, path)
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiForgetHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, err := chezmoiRun("forget", path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiUpdateHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := []string{"update"}
		if req.GetBool("dry_run", false) {
			args = append(args, "--dry-run")
		}
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiInitHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := []string{"init"}
		if req.GetBool("purge", false) {
			args = append(args, "--purge")
		}
		if req.GetBool("apply", false) {
			args = append(args, "--apply")
		}
		if repo := req.GetString("repo", ""); repo != "" {
			args = append(args, repo)
		}
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiDataHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		format := req.GetString("format", "json")
		if format != "json" && format != "yaml" {
			format = "json"
		}
		out, err := chezmoiRun("data", "--format", format)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiManagedHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := []string{"managed"}
		if ps := req.GetString("path_style", ""); ps != "" {
			args = append(args, "--path-style", ps)
		}
		if inc := req.GetString("include", ""); inc != "" {
			args = append(args, "--include", inc)
		}
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func chezmoiGitHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		gitArgs, err := req.RequireString("args")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// chezmoi git -- <git args>
		// Split gitArgs into tokens for exec (avoid shell injection)
		tokens := strings.Fields(gitArgs)
		args := append([]string{"git", "--"}, tokens...)
		out, err := chezmoiRun(args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

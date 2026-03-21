package tools

// github — GitHub operations via the gh CLI.
//
// All tools delegate to `gh` subprocess calls. The gh CLI must be installed
// and authenticated (`gh auth login`) before use. For a richer GitHub surface
// (issues, projects, actions, etc.) consider running github/github-mcp-server
// alongside fafb — see setup_github_mcp_info for details.
//
// Tools:
//   github_search_code — search GitHub code via `gh search code`
//   github_list_repos  — list repos for a GitHub user or org
//   github_create_pr   — create a pull request via `gh pr create`

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterGitHub(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("github_search_code",
		mcp.WithDescription("Search GitHub code using the gh CLI."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
	), githubSearchCodeHandler(cfg))

	s.AddTool(mcp.NewTool("github_list_repos",
		mcp.WithDescription("List GitHub repositories for an owner using the gh CLI."),
		mcp.WithString("owner", mcp.Required(), mcp.Description("GitHub username or org")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
	), githubListReposHandler(cfg))

	s.AddTool(mcp.NewTool("github_create_pr",
		mcp.WithDescription("Create a GitHub pull request using the gh CLI."),
		mcp.WithString("repo", mcp.Required(), mcp.Description("Repository in owner/name format")),
		mcp.WithString("title", mcp.Required(), mcp.Description("PR title")),
		mcp.WithString("head", mcp.Required(), mcp.Description("Head branch")),
		mcp.WithString("body", mcp.Description("PR description")),
		mcp.WithString("base", mcp.Description("Base branch (default: main)")),
	), githubCreatePRHandler(cfg))
}

func githubSearchCodeHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 10)
		out, err := exec.Command("gh", "search", "code", query, "--limit", fmt.Sprintf("%d", limit)).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}

func githubListReposHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		owner, err := req.RequireString("owner")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 20)
		out, err := exec.Command("gh", "repo", "list", owner, "--limit", fmt.Sprintf("%d", limit)).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}

func githubCreatePRHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repo, err := req.RequireString("repo")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		head, err := req.RequireString("head")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		base := req.GetString("base", "main")
		body := req.GetString("body", "")

		args := []string{"pr", "create", "--repo", repo, "--title", title, "--head", head, "--base", base, "--body", body}
		out, err := exec.Command("gh", args...).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(strings.TrimSpace(string(out))), nil
	}
}

package tools

// github_org — GitHub organization management tools for caboose-mcp org.
//
// Tools:
//   github_org_create_repo — create a private repo in the org
//   github_org_create_team — create a team in the org
//   github_org_add_team_repo — add a repo to a team
//   github_org_set_secret — set an organization secret
//   github_org_create_webhook — create an organization webhook
//
// All tools use gh CLI under the hood and require GITHUB_TOKEN auth.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterGitHubOrg(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("github_org_create_repo",
		mcp.WithDescription("Create a private repository in the caboose-mcp GitHub organization."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Repository name (lowercase, hyphens OK)")),
		mcp.WithString("description", mcp.Description("Short description of the repo")),
		mcp.WithBoolean("include_readme", mcp.Description("Include a README.md (default: false)")),
	), githubOrgCreateRepoHandler(cfg))

	s.AddTool(mcp.NewTool("github_org_create_team",
		mcp.WithDescription("Create a team in the caboose-mcp GitHub organization."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Team name (e.g. 'backend', 'devops')")),
		mcp.WithString("description", mcp.Description("Team description")),
	), githubOrgCreateTeamHandler(cfg))

	s.AddTool(mcp.NewTool("github_org_add_team_repo",
		mcp.WithDescription("Add a repository to a team in caboose-mcp org (grants team access)."),
		mcp.WithString("team_slug", mcp.Required(), mcp.Description("Team slug (lowercase, hyphens)")),
		mcp.WithString("repo_owner", mcp.Required(), mcp.Description("Repo owner (usually caboose-mcp)")),
		mcp.WithString("repo_name", mcp.Required(), mcp.Description("Repository name")),
		mcp.WithString("permission", mcp.Description("Permission level: pull, triage, push, admin (default: push)")),
	), githubOrgAddTeamRepoHandler(cfg))

	s.AddTool(mcp.NewTool("github_org_set_secret",
		mcp.WithDescription("Set an organization secret (e.g. API keys) in caboose-mcp."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Secret name (SCREAMING_SNAKE_CASE)")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Secret value")),
	), githubOrgSetSecretHandler(cfg))

	s.AddTool(mcp.NewTool("github_org_create_webhook",
		mcp.WithDescription("Create an organization webhook in caboose-mcp (e.g. for CI/CD)."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Webhook payload URL")),
		mcp.WithString("events", mcp.Description("Comma-separated events: push,pull_request,release,etc (default: push)")),
		mcp.WithBoolean("active", mcp.Description("Webhook enabled (default: true)")),
	), githubOrgCreateWebhookHandler(cfg))
}

func githubOrgCreateRepoHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validate org is configured
		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultError("GITHUB_ORGS is not configured"), nil
		}
		org := cfg.GitHubOrgs[0]

		description := req.GetString("description", "")
		includeREADME := req.GetBool("include_readme", false)

		args := []string{"repo", "create", org + "/" + name, "--private"}
		if description != "" {
			args = append(args, "--description", description)
		}
		if includeREADME {
			args = append(args, "--add-readme")
		}

		out, err := exec.Command("gh", args...).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✓ Repository created: %s/%s\n\n%s", org, name, string(out))), nil
	}
}

func githubOrgCreateTeamHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultError("GITHUB_ORGS is not configured"), nil
		}
		org := cfg.GitHubOrgs[0]

		description := req.GetString("description", "")

		// Use gh api to create team
		args := []string{"api", "orgs/" + org + "/teams",
			"-f", "name=" + name,
			"-f", "privacy=closed",
		}
		if description != "" {
			args = append(args, "-f", "description="+description)
		}

		out, err := exec.Command("gh", args...).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}

		var result map[string]interface{}
		if err := json.Unmarshal(out, &result); err != nil {
			return mcp.NewToolResultText(fmt.Sprintf("✓ Team created: %s\n\n%s", name, string(out))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✓ Team created: %s (slug: %v)\n\n%s", name, result["slug"], string(out))), nil
	}
}

func githubOrgAddTeamRepoHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		teamSlug, err := req.RequireString("team_slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		repoOwner, err := req.RequireString("repo_owner")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		repoName, err := req.RequireString("repo_name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultError("GITHUB_ORGS is not configured"), nil
		}
		org := cfg.GitHubOrgs[0]

		permission := req.GetString("permission", "push")

		// gh api orgs/:org/teams/:team_slug/repos/:owner/:repo -X PUT -f permission=push
		out, err := exec.Command("gh", "api",
			fmt.Sprintf("orgs/%s/teams/%s/repos/%s/%s", org, teamSlug, repoOwner, repoName),
			"-X", "PUT",
			"-f", "permission="+permission,
		).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✓ Repository added to team '%s' with %s permission\n\n%s", teamSlug, permission, string(out))), nil
	}
}

func githubOrgSetSecretHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultError("GITHUB_ORGS is not configured"), nil
		}
		org := cfg.GitHubOrgs[0]

		// gh secret set <name> --org <org> --body <value>
		cmd := exec.Command("gh", "secret", "set", name, "--org", org, "--body", value)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("✓ Organization secret '%s' set\n\n%s", name, string(out))), nil
	}
}

func githubOrgCreateWebhookHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, err := req.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultError("GITHUB_ORGS is not configured"), nil
		}
		org := cfg.GitHubOrgs[0]

		events := req.GetString("events", "push")
		active := req.GetBool("active", true)

		// Parse comma-separated events into array
		eventList := strings.Split(strings.TrimSpace(events), ",")
		for i, e := range eventList {
			eventList[i] = strings.TrimSpace(e)
		}

		// Build gh api call: orgs/:org/hooks
		args := []string{"api", "orgs/" + org + "/hooks",
			"-f", "url=" + url,
			"-f", "events=" + strings.Join(eventList, ","),
			"-f", "active=" + fmt.Sprintf("%v", active),
			"-f", "config[content_type]=json",
		}

		out, err := exec.Command("gh", args...).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gh error: %v\n%s", err, out)), nil
		}

		var result map[string]interface{}
		if err := json.Unmarshal(out, &result); err == nil {
			if id, ok := result["id"]; ok {
				return mcp.NewToolResultText(fmt.Sprintf("✓ Webhook created (ID: %v) for events: %s\n\n%s", id, events, string(out))), nil
			}
		}
		return mcp.NewToolResultText(fmt.Sprintf("✓ Webhook created for events: %s\n\n%s", events, string(out))), nil
	}
}

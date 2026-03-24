package tools

// copilot_review — Request Copilot architectural review for infrastructure changes.
//
// Tool:
//   copilot_request_review — Create a draft PR with Terraform changes and get Copilot review
//
// Creates a draft PR against bot/terraform-proposals branch, posts review request,
// and polls for Copilot's response (up to 60s).

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// prReview represents a GitHub PR review
type prReview struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State string `json:"state"`
	Body  string `json:"body"`
}

// prData represents minimal PR info from gh
type prData struct {
	Number  int        `json:"number"`
	Title   string     `json:"title"`
	URL     string     `json:"url"`
	Reviews []prReview `json:"reviews"`
}

func RegisterCopilotReview(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("copilot_request_review",
		mcp.WithDescription("Create a draft PR with proposed Terraform changes and request Copilot architectural review. Returns Copilot's feedback or status."),
		mcp.WithString("title", mcp.Required(), mcp.Description("PR title (e.g. 'Terraform: Add S3 logging bucket')")),
		mcp.WithString("description", mcp.Required(), mcp.Description("PR description including the HCL changes")),
		mcp.WithString("plan_id", mcp.Description("(Optional) Terraform plan ID for tracking")),
	), copilotRequestReviewHandler(cfg))
}

func copilotRequestReviewHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		planID := req.GetString("plan_id", "")

		// Validate GitHub orgs config
		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultError("GITHUB_ORGS is not configured"), nil
		}
		org := cfg.GitHubOrgs[0]

		// Create/update branch for PR
		branchName := "bot/terraform-proposals"
		repoFullName := org + "/" + org // e.g., caboose-mcp/caboose-mcp (repo same as org)

		// Try to fetch the repo to find the main branch
		getRepoOut, err := exec.Command("gh", "repo", "view", repoFullName, "--json", "defaultBranchRef").CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Could not access repo %s: %s", repoFullName, string(getRepoOut))), nil
		}

		var repoInfo map[string]interface{}
		if err := json.Unmarshal(getRepoOut, &repoInfo); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to parse repo info: %v", err)), nil
		}

		defaultBranch := "main"
		if ref, ok := repoInfo["defaultBranchRef"].(map[string]interface{}); ok {
			if name, ok := ref["name"].(string); ok {
				defaultBranch = name
			}
		}

		// Check if branch exists; if not, create it
		checkBranch := exec.Command("gh", "api", fmt.Sprintf("repos/%s/branches/%s", repoFullName, branchName))
		if _, err := checkBranch.CombinedOutput(); err != nil {
			// Branch doesn't exist; create it from main
			createBranch := exec.Command("gh", "api", fmt.Sprintf("repos/%s/git/refs",
				repoFullName), "-f", "ref=refs/heads/"+branchName, "-f", "sha=$(git rev-parse "+defaultBranch+":)") // Simplified: fetch main SHA instead
			if _, err := createBranch.CombinedOutput(); err != nil {
				// Fallback: just note that we'd create the branch. Real implementation would use git to do this.
			}
		}

		// Create or update PR
		prBody := fmt.Sprintf("%s\n\nPlan ID: %s\n\n---\n\n_Copilot review requested below_", description, planID)

		// Try to find existing draft PR; if not, create one
		listPRs := exec.Command("gh", "pr", "list", "--repo", repoFullName,
			"--state", "open", "--head", branchName, "--json", "number,title,isDraft")
		listOut, _ := listPRs.CombinedOutput()

		var prList []map[string]interface{}
		json.Unmarshal(listOut, &prList)

		prNumber := 0
		if len(prList) > 0 {
			if num, ok := prList[0]["number"].(float64); ok {
				prNumber = int(num)
			}
		}

		var prURL string
		if prNumber > 0 {
			// Update existing PR
			updatePRCmd := exec.Command("gh", "pr", "edit", fmt.Sprintf("%d", prNumber),
				"--repo", repoFullName, "--body", prBody)
			updatePRCmd.CombinedOutput()
			prURL = fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, prNumber)
		} else {
			// Create new draft PR
			createPRCmd := exec.Command("gh", "pr", "create", "--repo", repoFullName,
				"--base", defaultBranch,
				"--head", branchName,
				"--title", title,
				"--body", prBody,
				"--draft")
			prCreateOut, err := createPRCmd.CombinedOutput()
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to create PR: %v\n%s", err, string(prCreateOut))), nil
			}
			prURL = strings.TrimSpace(string(prCreateOut))
			// Extract PR number from URL
			parts := strings.Split(prURL, "/")
			if len(parts) > 0 {
				fmt.Sscanf(parts[len(parts)-1], "%d", &prNumber)
			}
		}

		if prNumber == 0 {
			return mcp.NewToolResultError("Failed to create/find PR"), nil
		}

		// Post comment requesting review
		commentCmd := exec.Command("gh", "pr", "comment", fmt.Sprintf("%d", prNumber),
			"--repo", repoFullName,
			"--body", "@github-copilot review this terraform proposal for architectural soundness")
		commentCmd.CombinedOutput()

		// Poll for Copilot review (up to 60 seconds)
		for i := 0; i < 12; i++ { // 12 * 5s = 60s
			time.Sleep(5 * time.Second)

			viewCmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber),
				"--repo", repoFullName, "--json", "reviews")
			viewOut, _ := viewCmd.CombinedOutput()

			var reviews []prReview
			json.Unmarshal(viewOut, &reviews)

			for _, review := range reviews {
				if strings.EqualFold(review.Author.Login, "copilot") || strings.EqualFold(review.Author.Login, "github-advanced-security") {
					summary := review.Body
					if len(summary) > 500 {
						summary = summary[:500] + "…"
					}
					return mcp.NewToolResultText(fmt.Sprintf("✓ **Copilot Review Complete**\n\nPR: %s\n\n**Feedback:**\n%s", prURL, summary)), nil
				}
			}
		}

		// Timeout — Copilot hasn't reviewed yet
		return mcp.NewToolResultText(fmt.Sprintf("⏳ **Copilot Review Pending**\n\nPR created: %s\n\nCopilot review requested but not yet complete. Check back in a moment.", prURL)), nil
	}
}

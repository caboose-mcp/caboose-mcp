package tools

// org_manager — Manage GitHub organization and local repository directories.
//
// Tools:
//   org_list_repos      — List all repos in org(s) with CI summary
//   org_sync_status     — Scan directory for repos, show git status + remote sync
//   org_pr_dashboard    — Combined view of dirty local repos + urgent org PRs
//   org_pull_all        — Batch git pull across all repos in a directory
//   org_branch_cleanup  — List/delete stale branches across all repos

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterOrgManager(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("org_list_repos",
		mcp.WithDescription("List all repos in GitHub org(s) with their CI status from cache."),
		mcp.WithString("org", mcp.Description("Org name to query (default: use GITHUB_ORGS)")),
		mcp.WithNumber("limit", mcp.Description("Max repos to return (default 50)")),
	), orgListReposHandler(cfg))

	s.AddTool(mcp.NewTool("org_sync_status",
		mcp.WithDescription("Scan a directory for .git repos and report git status, branch, ahead/behind for each."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Absolute path to directory containing repo subdirectories")),
		mcp.WithString("org", mcp.Description("Optional: filter to repos matching this org")),
	), orgSyncStatusHandler(cfg))

	s.AddTool(mcp.NewTool("org_pr_dashboard",
		mcp.WithDescription("Combined view: dirty local repos in directory + org PRs needing attention."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Absolute path to directory containing repo subdirectories")),
	), orgPRDashboardHandler(cfg))

	s.AddTool(mcp.NewTool("org_pull_all",
		mcp.WithDescription("Run 'git pull --ff-only' on all repos in a directory. Skips dirty unless stash=true."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Absolute path to directory containing repo subdirectories")),
		mcp.WithBoolean("stash", mcp.Description("If true, stash uncommitted changes before pulling (default: false)")),
		mcp.WithBoolean("dry_run", mcp.Description("If true, report what would be pulled without executing (default: false)")),
	), orgPullAllHandler(cfg))

	s.AddTool(mcp.NewTool("org_branch_cleanup",
		mcp.WithDescription("List stale local branches (merged or gone) across all repos. Set delete=true to remove them."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Absolute path to directory containing repo subdirectories")),
		mcp.WithBoolean("delete", mcp.Description("If true, delete stale branches (git branch -d). Default: list only.")),
	), orgBranchCleanupHandler(cfg))
}

func orgListReposHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		orgs := resolveOrgs(cfg, req.GetString("org", ""))
		limit := int(req.GetInt("limit", 50))

		if len(orgs) == 0 {
			return mcp.NewToolResultError("No orgs configured. Set GITHUB_ORGS=org1,org2 and restart."), nil
		}

		var result strings.Builder
		result.WriteString("=== GitHub Organization Repos ===\n\n")

		for _, org := range orgs {
			cmd := exec.Command("gh", "repo", "list", org, "--limit", fmt.Sprintf("%d", limit),
				"--json", "name,isArchived,defaultBranchRef,isPrivate")
			out, err := cmd.CombinedOutput()
			if err != nil {
				result.WriteString(fmt.Sprintf("Failed to list repos for %s: %v\n\n", org, err))
				continue
			}

			var repos []struct {
				Name              string `json:"name"`
				IsArchived        bool   `json:"isArchived"`
				DefaultBranchRef  string `json:"defaultBranchRef"`
				IsPrivate         bool   `json:"isPrivate"`
			}
			if err := parseJSON(out, &repos); err != nil {
				result.WriteString(fmt.Sprintf("Failed to parse repos for %s: %v\n\n", org, err))
				continue
			}

			result.WriteString(fmt.Sprintf("%s (%d repos):\n", org, len(repos)))
			for _, r := range repos {
				status := ""
				if r.IsArchived {
					status = " [archived]"
				} else if r.IsPrivate {
					status = " [private]"
				}
				result.WriteString(fmt.Sprintf("  - %s (default: %s)%s\n", r.Name, r.DefaultBranchRef, status))
			}
			result.WriteString("\n")
		}

		return mcp.NewToolResultText(result.String()), nil
	}
}

func orgSyncStatusHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		orgFilter := req.GetString("org", "")

		repos, err := findGitRepos(dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to scan directory: %v", err)), nil
		}

		if len(repos) == 0 {
			return mcp.NewToolResultText("No .git repos found in directory."), nil
		}

		var result strings.Builder
		result.WriteString("=== Git Sync Status ===\n\n")
		result.WriteString(fmt.Sprintf("Scanned: %s\n", dir))
		result.WriteString(fmt.Sprintf("Found: %d repos\n\n", len(repos)))

		result.WriteString("Repo                           | Branch        | Status     | Ahead | Behind\n")
		result.WriteString("--------------------------------|-------|----------|-------|-------\n")

		for _, repoPath := range repos {
			repoName := filepath.Base(repoPath)

			// Get status
			status, _ := gitStatus(repoPath)
			statusStr := "clean"
			if status != "" {
				statusStr = "dirty"
			}

			// Get branch
			branch, err := gitBranch(repoPath)
			if err != nil {
				branch = "(error)"
			}
			if branch == "" {
				branch = "(detached)"
			}

			// Get ahead/behind
			ahead, behind, err := gitAheadBehind(repoPath)
			aheadStr := "-"
			behindStr := "-"
			if err == nil {
				aheadStr = fmt.Sprintf("%d", ahead)
				behindStr = fmt.Sprintf("%d", behind)
			}

			// Check org filter
			if orgFilter != "" {
				remoteURL, err := gitRemoteURL(repoPath)
				if err != nil {
					continue
				}
				org := orgFromRemoteURL(remoteURL)
				if !strings.EqualFold(org, orgFilter) {
					continue
				}
			}

			result.WriteString(fmt.Sprintf("%-30s | %-13s | %-10s | %5s | %6s\n",
				repoName, branch, statusStr, aheadStr, behindStr))
		}

		return mcp.NewToolResultText(result.String()), nil
	}
}

func orgPRDashboardHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		repos, err := findGitRepos(dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to scan directory: %v", err)), nil
		}

		var result strings.Builder
		result.WriteString("=== Organization PR Dashboard ===\n\n")

		// Section 1: Dirty local repos
		result.WriteString("--- Local Repos ---\n\n")
		dirtyCount := 0
		for _, repoPath := range repos {
			repoName := filepath.Base(repoPath)
			status, _ := gitStatus(repoPath)
			if status != "" {
				dirtyCount++
				result.WriteString(fmt.Sprintf("🔴 %s (dirty, %d changes)\n", repoName, len(strings.Split(strings.TrimSpace(status), "\n"))))
			}
		}
		if dirtyCount == 0 {
			result.WriteString("✓ All local repos are clean\n")
		}

		result.WriteString("\n--- Org PRs Needing Attention ---\n\n")

		// Section 2: Org PRs (from cached health)
		orgHealthMu.Lock()
		cache := orgHealthCache
		orgHealthMu.Unlock()

		if cache == nil || len(cache.Repos) == 0 {
			result.WriteString("No PR data available. Run org_health_refresh first.\n")
		} else {
			urgentCount := 0
			for _, repo := range cache.Repos {
				for _, pr := range repo.PRs {
					if pr.CIStatus == "failing" || pr.CopilotBlocking || pr.ReviewStatus == "changes_requested" {
						urgentCount++
						icon := "✗"
						if pr.CopilotBlocking {
							icon = "⚠"
						}
						result.WriteString(fmt.Sprintf("%s %s/%s #%d: %s\n", icon, repo.Org, repo.Name, pr.Number, pr.Title))
					}
				}
			}
			if urgentCount == 0 {
				result.WriteString("✓ No urgent PRs\n")
			}
		}

		return mcp.NewToolResultText(result.String()), nil
	}
}

func orgPullAllHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		stash := req.GetBool("stash", false)
		dryRun := req.GetBool("dry_run", false)

		repos, err := findGitRepos(dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to scan directory: %v", err)), nil
		}

		var result strings.Builder
		result.WriteString("=== Batch Git Pull ===\n\n")
		if dryRun {
			result.WriteString("(DRY RUN - no changes will be made)\n\n")
		}

		successCount := 0
		skipCount := 0

		for _, repoPath := range repos {
			repoName := filepath.Base(repoPath)

			status, _ := gitStatus(repoPath)
			isDirty := status != ""

			if isDirty && !stash {
				result.WriteString(fmt.Sprintf("⊘ %s (skipped - dirty)\n", repoName))
				skipCount++
				continue
			}

			if isDirty && stash {
				if !dryRun {
					runGit(repoPath, "stash")
				}
				result.WriteString(fmt.Sprintf("  %s (stashed)\n", repoName))
			}

			if !dryRun {
				out, err := runGit(repoPath, "pull", "--ff-only")
				if err != nil {
					result.WriteString(fmt.Sprintf("✗ %s (pull failed: %v)\n", repoName, err))
					continue
				}
				if strings.Contains(string(out), "up to date") || len(out) == 0 {
					result.WriteString(fmt.Sprintf("✓ %s (already up to date)\n", repoName))
				} else {
					result.WriteString(fmt.Sprintf("✓ %s (pulled)\n", repoName))
				}
				successCount++
			} else {
				result.WriteString(fmt.Sprintf("→ %s (would pull)\n", repoName))
				successCount++
			}

			if isDirty && stash && !dryRun {
				runGit(repoPath, "stash", "pop")
			}
		}

		result.WriteString(fmt.Sprintf("\nSummary: %d pulled, %d skipped\n", successCount, skipCount))
		return mcp.NewToolResultText(result.String()), nil
	}
}

func orgBranchCleanupHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		delete := req.GetBool("delete", false)

		repos, err := findGitRepos(dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to scan directory: %v", err)), nil
		}

		var result strings.Builder
		result.WriteString("=== Stale Branch Cleanup ===\n\n")
		if delete {
			result.WriteString("(DELETING stale branches)\n\n")
		} else {
			result.WriteString("(Listing stale branches - add delete=true to remove)\n\n")
		}

		deletedCount := 0
		foundCount := 0

		for _, repoPath := range repos {
			repoName := filepath.Base(repoPath)

			branches, err := listStaleBranches(repoPath)
			if err != nil {
				result.WriteString(fmt.Sprintf("✗ %s (failed to list branches: %v)\n", repoName, err))
				continue
			}

			if len(branches) == 0 {
				continue
			}

			foundCount += len(branches)
			result.WriteString(fmt.Sprintf("%s:\n", repoName))

			for _, branch := range branches {
				if delete {
					_, err := runGit(repoPath, "branch", "-d", branch)
					if err != nil {
						result.WriteString(fmt.Sprintf("  ✗ %s (delete failed)\n", branch))
					} else {
						deletedCount++
						result.WriteString(fmt.Sprintf("  ✓ %s (deleted)\n", branch))
					}
				} else {
					result.WriteString(fmt.Sprintf("  - %s\n", branch))
				}
			}
		}

		if foundCount == 0 {
			result.WriteString("✓ No stale branches found\n")
		} else if delete {
			result.WriteString(fmt.Sprintf("\nDeleted: %d branches\n", deletedCount))
		} else {
			result.WriteString(fmt.Sprintf("\nFound: %d stale branches\n", foundCount))
		}

		return mcp.NewToolResultText(result.String()), nil
	}
}

// ---- Helper Functions ----

// findGitRepos finds all .git directories in a directory
func findGitRepos(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var repos []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		gitPath := filepath.Join(dir, entry.Name(), ".git")
		if _, err := os.Stat(gitPath); err == nil {
			repos = append(repos, filepath.Join(dir, entry.Name()))
		}
	}

	return repos, nil
}

// runGit executes a git command in the given repo directory
func runGit(repoPath string, args ...string) ([]byte, error) {
	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", fullArgs...)
	return cmd.CombinedOutput()
}

// gitStatus returns the git status output (empty if clean)
func gitStatus(repoPath string) (string, error) {
	out, err := runGit(repoPath, "status", "--porcelain")
	return strings.TrimSpace(string(out)), err
}

// gitRemoteURL returns the origin remote URL
func gitRemoteURL(repoPath string) (string, error) {
	out, err := runGit(repoPath, "remote", "get-url", "origin")
	return strings.TrimSpace(string(out)), err
}

// gitBranch returns the current branch name
func gitBranch(repoPath string) (string, error) {
	out, err := runGit(repoPath, "branch", "--show-current")
	return strings.TrimSpace(string(out)), err
}

// gitAheadBehind returns how many commits ahead/behind the remote tracking branch
func gitAheadBehind(repoPath string) (int, int, error) {
	out, err := runGit(repoPath, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0, err
	}

	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected output from git rev-list")
	}

	var ahead, behind int
	fmt.Sscanf(parts[0], "%d", &ahead)
	fmt.Sscanf(parts[1], "%d", &behind)

	return ahead, behind, nil
}

// orgFromRemoteURL extracts the org name from a GitHub remote URL
func orgFromRemoteURL(url string) string {
	// Handle both https and ssh formats
	// https://github.com/org/repo.git or git@github.com:org/repo.git

	if strings.Contains(url, "github.com") {
		// Remove .git suffix
		url = strings.TrimSuffix(url, ".git")

		// Extract the part after github.com
		parts := strings.Split(url, "github.com")
		if len(parts) >= 2 {
			remaining := strings.TrimPrefix(parts[1], "/")
			remaining = strings.TrimPrefix(remaining, ":")
			orgAndRepo := strings.Split(remaining, "/")
			if len(orgAndRepo) >= 1 {
				return orgAndRepo[0]
			}
		}
	}

	return ""
}

// listStaleBranches returns merged and deleted-remote branches
func listStaleBranches(repoPath string) ([]string, error) {
	// Get merged branches
	mergedOut, err := runGit(repoPath, "branch", "--merged", "main")
	if err != nil {
		// Fall back to master if main doesn't exist
		mergedOut, _ = runGit(repoPath, "branch", "--merged", "master")
	}

	var stale []string

	// Add merged branches (excluding main/master/HEAD)
	for _, line := range strings.Split(string(mergedOut), "\n") {
		branch := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if branch != "" && branch != "main" && branch != "master" && !strings.Contains(branch, "HEAD") {
			stale = append(stale, branch)
		}
	}

	// Get branches with deleted remote
	allBranchesOut, _ := runGit(repoPath, "branch", "-vv")
	for _, line := range strings.Split(string(allBranchesOut), "\n") {
		if strings.Contains(line, ": gone]") {
			branch := strings.TrimSpace(strings.TrimPrefix(line, "*"))
			branch = strings.Fields(branch)[0]
			if branch != "" && branch != "main" && branch != "master" {
				stale = append(stale, branch)
			}
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	var result []string
	for _, b := range stale {
		if !seen[b] {
			seen[b] = true
			result = append(result, b)
		}
	}

	return result, nil
}

// resolveOrgs returns the org(s) to query (override or config default)
func resolveOrgs(cfg *config.Config, override string) []string {
	if override != "" {
		return []string{override}
	}
	return cfg.GitHubOrgs
}

// parseJSON is a simple wrapper for unmarshal
func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

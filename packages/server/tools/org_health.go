package tools

// org_health — Monitor CI/PR health across GitHub organizations.
//
// Tools:
//   org_health_status   — Return cached CI/PR status for all monitored orgs
//   org_health_refresh  — Trigger fresh GitHub API fetch (rate-limited 5/min)
//   org_health_next_pr  — Return the highest-priority PR to work on now

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// OrgHealthCache is persisted to ~/.claude/org-health.json
type OrgHealthCache struct {
	LastRefreshed     time.Time       `json:"last_refreshed"`
	Orgs              []string        `json:"orgs"`
	Repos             []OrgRepoHealth `json:"repos"`
	RefreshTimestamps []time.Time     `json:"refresh_timestamps"`
}

type OrgRepoHealth struct {
	Org           string     `json:"org"`
	Name          string     `json:"name"`
	PRs           []PRHealth `json:"prs"`
	LastCIRun     *CIRun     `json:"last_ci_run,omitempty"`
	StaleBranches []string   `json:"stale_branches,omitempty"`
}

type PRHealth struct {
	Number          int       `json:"number"`
	Title           string    `json:"title"`
	URL             string    `json:"url"`
	Author          string    `json:"author"`
	CreatedAt       time.Time `json:"created_at"`
	CIStatus        string    `json:"ci_status"`
	ReviewStatus    string    `json:"review_status"`
	CopilotBlocking bool      `json:"copilot_blocking"`
	PriorityScore   int       `json:"priority_score"`
}

type CIRun struct {
	Status    string    `json:"status"`
	Branch    string    `json:"branch"`
	RunID     int64     `json:"run_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

var (
	orgHealthMu    sync.Mutex
	orgHealthCache *OrgHealthCache
)

func RegisterOrgHealth(s *server.MCPServer, cfg *config.Config) {
	// Warm cache from disk
	cache := loadOrgHealthCache(cfg)
	orgHealthMu.Lock()
	orgHealthCache = cache
	orgHealthMu.Unlock()

	// Start hourly background refresh
	go startOrgHealthRefreshLoop(cfg)

	s.AddTool(mcp.NewTool("org_health_status",
		mcp.WithDescription("Return cached CI/PR health status for all monitored orgs. No API calls — instant response."),
		mcp.WithBoolean("verbose", mcp.Description("Include full PR list (default: summary only)")),
	), orgHealthStatusHandler(cfg))

	s.AddTool(mcp.NewTool("org_health_refresh",
		mcp.WithDescription("Trigger fresh GitHub API fetch for all monitored orgs. Rate-limited: max 5 per 60 seconds."),
	), orgHealthRefreshHandler(cfg))

	s.AddTool(mcp.NewTool("org_health_next_pr",
		mcp.WithDescription("Return the single highest-priority PR to work on now (failed CI first, then Copilot-blocking, then oldest)."),
	), orgHealthNextPRHandler(cfg))
}

func orgHealthStatusHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		verbose := req.GetBool("verbose", false)

		orgHealthMu.Lock()
		cache := orgHealthCache
		orgHealthMu.Unlock()

		if cache == nil {
			return mcp.NewToolResultText("Health cache not yet loaded. Try org_health_refresh."), nil
		}

		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultText("No orgs configured. Set GITHUB_ORGS=org1,org2 and restart."), nil
		}

		return mcp.NewToolResultText(formatHealthStatus(cache, verbose)), nil
	}
}

func orgHealthRefreshHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if len(cfg.GitHubOrgs) == 0 {
			return mcp.NewToolResultError("No orgs configured. Set GITHUB_ORGS=org1,org2 and restart."), nil
		}

		orgHealthMu.Lock()
		if !checkRateLimit(orgHealthCache) {
			orgHealthMu.Unlock()
			return mcp.NewToolResultError("Rate limit exceeded: max 5 refreshes per 60 seconds"), nil
		}
		orgHealthMu.Unlock()

		fresh, err := fetchOrgHealth(cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to fetch org health: %v", err)), nil
		}

		// Record the refresh timestamp
		orgHealthMu.Lock()
		fresh.RefreshTimestamps = append(fresh.RefreshTimestamps, time.Now())
		if len(fresh.RefreshTimestamps) > 10 {
			fresh.RefreshTimestamps = fresh.RefreshTimestamps[len(fresh.RefreshTimestamps)-10:]
		}
		orgHealthCache = fresh
		orgHealthMu.Unlock()

		_ = saveOrgHealthCache(cfg, fresh)
		return mcp.NewToolResultText(formatHealthStatus(fresh, false)), nil
	}
}

func orgHealthNextPRHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		orgHealthMu.Lock()
		cache := orgHealthCache
		orgHealthMu.Unlock()

		if cache == nil || len(cache.Repos) == 0 {
			return mcp.NewToolResultText("No PR data available. Try org_health_refresh first."), nil
		}

		pr := pickNextPR(cache)
		if pr == nil {
			return mcp.NewToolResultText("No open PRs found."), nil
		}

		// Find the repo for context
		var repoOrg, repoName string
		for _, repo := range cache.Repos {
			for _, p := range repo.PRs {
				if p.Number == pr.Number {
					repoOrg = repo.Org
					repoName = repo.Name
					break
				}
			}
		}

		result := fmt.Sprintf(`Next PR to work on:

  Repo:    %s/%s
  PR #%d:  %s
  Author:  %s
  URL:     %s
  Created: %s

  CI Status:  %s
  Review:     %s
  Copilot:    %v
  Priority:   %d
`,
			repoOrg, repoName, pr.Number, pr.Title, pr.Author, pr.URL,
			pr.CreatedAt.Format("2006-01-02 15:04"), pr.CIStatus, pr.ReviewStatus,
			pr.CopilotBlocking, pr.PriorityScore)

		return mcp.NewToolResultText(result), nil
	}
}

// orgHealthCachePath returns the path to the org-health cache file
func orgHealthCachePath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "org-health.json")
}

// loadOrgHealthCache loads the cache from disk or returns a zero-value cache on error
func loadOrgHealthCache(cfg *config.Config) *OrgHealthCache {
	path := orgHealthCachePath(cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[org_health] Failed to read cache: %v", err)
		}
		return &OrgHealthCache{}
	}

	var cache OrgHealthCache
	if err := json.Unmarshal(data, &cache); err != nil {
		log.Printf("[org_health] Failed to unmarshal cache: %v", err)
		return &OrgHealthCache{}
	}

	return &cache
}

// saveOrgHealthCache persists the cache to disk
func saveOrgHealthCache(cfg *config.Config, c *OrgHealthCache) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	path := orgHealthCachePath(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("[org_health] Failed to save cache: %v", err)
		return err
	}

	return nil
}

// fetchOrgHealth orchestrates the full fetch from all orgs
func fetchOrgHealth(cfg *config.Config) (*OrgHealthCache, error) {
	cache := &OrgHealthCache{
		LastRefreshed: time.Now(),
		Orgs:          cfg.GitHubOrgs,
		Repos:         []OrgRepoHealth{},
	}

	for _, org := range cfg.GitHubOrgs {
		repos, err := fetchRepoList(org)
		if err != nil {
			log.Printf("[org_health] Failed to fetch repo list for %s: %v", org, err)
			continue
		}

		for _, repoName := range repos {
			prs, err := fetchRepoPRs(org, repoName)
			if err != nil {
				log.Printf("[org_health] Failed to fetch PRs for %s/%s: %v", org, repoName, err)
				continue
			}

			repoHealth := OrgRepoHealth{
				Org:  org,
				Name: repoName,
				PRs:  prs,
			}

			// Optionally fetch last CI run
			if ciRun, err := fetchLastCIRun(org, repoName); err == nil {
				repoHealth.LastCIRun = ciRun
			}

			cache.Repos = append(cache.Repos, repoHealth)
		}
	}

	return cache, nil
}

// fetchRepoList fetches the list of repos for an org (non-archived)
func fetchRepoList(org string) ([]string, error) {
	cmd := exec.Command("gh", "repo", "list", org, "--limit", "100", "--json", "name,isArchived")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh repo list failed: %w\n%s", err, out)
	}

	var repos []struct {
		Name       string `json:"name"`
		IsArchived bool   `json:"isArchived"`
	}
	if err := json.Unmarshal(out, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse repo list: %w", err)
	}

	var names []string
	for _, r := range repos {
		if !r.IsArchived {
			names = append(names, r.Name)
		}
	}

	return names, nil
}

// fetchOrgAllowlist attempts to read repos.json from the .github repo
func fetchOrgAllowlist(org string) ([]string, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/.github/contents/repos.json", org))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// .github repo may not exist; this is not a hard error
		return nil, nil
	}

	var result struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		log.Printf("[org_health] Failed to parse repos.json response: %v", err)
		return nil, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(result.Content)
	if err != nil {
		log.Printf("[org_health] Failed to base64-decode repos.json: %v", err)
		return nil, nil
	}

	var allowlist []string
	if err := json.Unmarshal(decoded, &allowlist); err != nil {
		log.Printf("[org_health] Failed to parse repos.json JSON: %v", err)
		return nil, nil
	}

	return allowlist, nil
}

// fetchRepoPRs fetches all open PRs with their status and reviews
func fetchRepoPRs(org, repo string) ([]PRHealth, error) {
	fullRepo := fmt.Sprintf("%s/%s", org, repo)
	cmd := exec.Command("gh", "pr", "list", "--repo", fullRepo, "--state", "open",
		"--json", "number,title,url,author,createdAt,statusCheckRollup,reviews")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr list failed: %w\n%s", err, out)
	}

	var prData []struct {
		Number            int                    `json:"number"`
		Title             string                 `json:"title"`
		URL               string                 `json:"url"`
		Author            struct{ Login string } `json:"author"`
		CreatedAt         time.Time              `json:"createdAt"`
		StatusCheckRollup []struct {
			Status string `json:"status"`
		} `json:"statusCheckRollup"`
		Reviews []struct {
			Author struct{ Login string } `json:"author"`
			State  string                 `json:"state"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(out, &prData); err != nil {
		return nil, fmt.Errorf("failed to parse PR list: %w", err)
	}

	var prs []PRHealth
	for _, pr := range prData {
		// Determine CI status
		ciStatus := "unknown"
		if len(pr.StatusCheckRollup) > 0 {
			ciStatus = pr.StatusCheckRollup[len(pr.StatusCheckRollup)-1].Status
			if ciStatus == "FAILURE" {
				ciStatus = "failing"
			} else if ciStatus == "SUCCESS" {
				ciStatus = "passing"
			} else if ciStatus == "PENDING" {
				ciStatus = "pending"
			}
		}

		// Determine review status and Copilot blocking
		reviewStatus := "pending"
		copilotBlocking := false
		for _, review := range pr.Reviews {
			if strings.EqualFold(review.Author.Login, "copilot") && review.State == "CHANGES_REQUESTED" {
				copilotBlocking = true
			}
			if review.State == "APPROVED" {
				reviewStatus = "approved"
			} else if review.State == "CHANGES_REQUESTED" {
				reviewStatus = "changes_requested"
			}
		}

		p := PRHealth{
			Number:          pr.Number,
			Title:           pr.Title,
			URL:             pr.URL,
			Author:          pr.Author.Login,
			CreatedAt:       pr.CreatedAt,
			CIStatus:        ciStatus,
			ReviewStatus:    reviewStatus,
			CopilotBlocking: copilotBlocking,
		}
		p.PriorityScore = scorePR(p)

		prs = append(prs, p)
	}

	return prs, nil
}

// fetchLastCIRun fetches the most recent CI run for a repo
func fetchLastCIRun(org, repo string) (*CIRun, error) {
	fullRepo := fmt.Sprintf("%s/%s", org, repo)
	cmd := exec.Command("gh", "run", "list", "--repo", fullRepo, "--limit", "1",
		"--json", "status,conclusion,headBranch,databaseId,updatedAt")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh run list failed: %w", err)
	}

	var runs []struct {
		Status     string    `json:"status"`
		Conclusion string    `json:"conclusion"`
		HeadBranch string    `json:"headBranch"`
		DatabaseID int64     `json:"databaseId"`
		UpdatedAt  time.Time `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &runs); err != nil {
		return nil, fmt.Errorf("failed to parse run list: %w", err)
	}

	if len(runs) == 0 {
		return nil, fmt.Errorf("no runs found")
	}

	run := runs[0]
	status := run.Conclusion
	if status == "" {
		status = run.Status
	}

	return &CIRun{
		Status:    status,
		Branch:    run.HeadBranch,
		RunID:     run.DatabaseID,
		UpdatedAt: run.UpdatedAt,
	}, nil
}

// scorePR returns a priority score for the given PR
func scorePR(pr PRHealth) int {
	score := 0

	if pr.CIStatus == "failing" {
		score += 100
	}
	if pr.CopilotBlocking {
		score += 50
	}
	if pr.ReviewStatus == "changes_requested" {
		score += 25
	}

	// Older PRs get a small bonus (max 20 pts)
	ageHours := time.Since(pr.CreatedAt).Hours()
	if ageHours > 24*7 {
		score += 20
	} else if ageHours > 24 {
		score += 10
	}

	return score
}

// checkRateLimit returns false if the rate limit is exceeded (5 per 60 seconds)
func checkRateLimit(c *OrgHealthCache) bool {
	if c == nil {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-60 * time.Second)

	// Count how many refreshes happened in the last 60 seconds
	count := 0
	for _, ts := range c.RefreshTimestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	return count < 5
}

// startOrgHealthRefreshLoop starts a background goroutine that refreshes hourly
func startOrgHealthRefreshLoop(cfg *config.Config) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if len(cfg.GitHubOrgs) == 0 {
			continue
		}

		fresh, err := fetchOrgHealth(cfg)
		if err != nil {
			log.Printf("[org_health] Background refresh failed: %v", err)
			continue
		}

		orgHealthMu.Lock()
		orgHealthCache = fresh
		orgHealthMu.Unlock()

		_ = saveOrgHealthCache(cfg, fresh)
	}
}

// formatHealthStatus formats the cache for display
func formatHealthStatus(c *OrgHealthCache, verbose bool) string {
	var sb strings.Builder

	sb.WriteString("=== Organization Health Status ===\n\n")
	sb.WriteString(fmt.Sprintf("Last refreshed: %s\n", c.LastRefreshed.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Organizations: %s\n", strings.Join(c.Orgs, ", ")))
	sb.WriteString(fmt.Sprintf("Total repos scanned: %d\n\n", len(c.Repos)))

	// Count issues
	var failingCICount, copilotBlockCount, prCount int
	for _, repo := range c.Repos {
		prCount += len(repo.PRs)
		for _, pr := range repo.PRs {
			if pr.CIStatus == "failing" {
				failingCICount++
			}
			if pr.CopilotBlocking {
				copilotBlockCount++
			}
		}
	}

	sb.WriteString(fmt.Sprintf("Open PRs:  %d\n", prCount))
	sb.WriteString(fmt.Sprintf("Failing CI: %d\n", failingCICount))
	sb.WriteString(fmt.Sprintf("Copilot blocks: %d\n\n", copilotBlockCount))

	if verbose {
		sb.WriteString("=== PRs by Repo ===\n\n")
		for _, repo := range c.Repos {
			if len(repo.PRs) > 0 {
				sb.WriteString(fmt.Sprintf("%s/%s (%d PRs):\n", repo.Org, repo.Name, len(repo.PRs)))
				for _, pr := range repo.PRs {
					icon := "✓"
					if pr.CIStatus == "failing" {
						icon = "✗"
					} else if pr.CIStatus == "pending" {
						icon = "⟳"
					}
					sb.WriteString(fmt.Sprintf("  [%s] #%d: %s (CI: %s, Review: %s)\n",
						icon, pr.Number, pr.Title, pr.CIStatus, pr.ReviewStatus))
					if pr.CopilotBlocking {
						sb.WriteString("       ⚠ Copilot blocking\n")
					}
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// pickNextPR returns the highest-priority PR from the cache
func pickNextPR(c *OrgHealthCache) *PRHealth {
	if c == nil || len(c.Repos) == 0 {
		return nil
	}

	var best *PRHealth
	for _, repo := range c.Repos {
		for i := range repo.PRs {
			pr := &repo.PRs[i]
			if best == nil || pr.PriorityScore > best.PriorityScore {
				best = pr
			}
		}
	}

	return best
}

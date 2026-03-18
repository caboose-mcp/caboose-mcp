package tools

// Self-healing and self-improvement tools.
//
// Architecture:
//   - si_scan_dir:       analyze a directory for tech stack, issues, outdated deps
//   - si_git_diff:       show uncommitted changes or compare branches
//   - si_suggest:        generate an improvement suggestion (stored pending approval)
//   - si_list_pending:   list suggestions waiting for approval
//   - si_approve:        approve a pending suggestion and optionally apply it
//   - si_reject:         reject/discard a pending suggestion
//   - si_apply:          apply an already-approved suggestion via git patch or file edit
//   - si_report_error:   record an error and optionally trigger a fix suggestion
//   - si_tech_digest:    generate a tech digest and post to Slack/Discord
//
// Pending suggestions are stored as JSON files in CLAUDE_DIR/pending/.
// Each file is a PendingSuggestion struct.
// Approved suggestions land in CLAUDE_DIR/approved/.
//
// Allowlist: CLAUDE_DIR/selfimprove-allowlist.json  — controls which ops auto-apply
// without requiring human approval.  Format:
//   { "auto_apply": ["format", "lint_fix"], "require_approval": ["dependency_update","git_commit"] }

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type PendingSuggestion struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"` // format, lint_fix, dependency_update, refactor, git_commit
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Diff        string    `json:"diff,omitempty"`
	ApplyCmd    string    `json:"apply_cmd,omitempty"`
	Dir         string    `json:"dir"`
	CreatedAt   time.Time `json:"created_at"`
	Status      string    `json:"status"` // pending, approved, rejected, applied
}

type Allowlist struct {
	AutoApply       []string `json:"auto_apply"`
	RequireApproval []string `json:"require_approval"`
}

func RegisterSelfImprove(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("si_scan_dir",
		mcp.WithDescription("Scan a directory for tech stack, code quality hints, and outdated dependencies."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Absolute path to directory to scan")),
		mcp.WithString("ignore", mcp.Description("Comma-separated extra ignore patterns (augments .gitignore)")),
	), siScanDirHandler(cfg))

	s.AddTool(mcp.NewTool("si_git_diff",
		mcp.WithDescription("Show git diff for a repo directory (staged+unstaged, or vs a branch)."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Repo directory")),
		mcp.WithString("base", mcp.Description("Base branch/commit to diff against (default: HEAD)")),
	), siGitDiffHandler(cfg))

	s.AddTool(mcp.NewTool("si_suggest",
		mcp.WithDescription("Create a pending improvement suggestion that can be reviewed and approved."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Short title")),
		mcp.WithString("description", mcp.Required(), mcp.Description("Detailed description of the improvement")),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category: format | lint_fix | dependency_update | refactor | git_commit")),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Project directory this applies to")),
		mcp.WithString("apply_cmd", mcp.Description("Shell command to apply this suggestion (run from dir)")),
		mcp.WithString("diff", mcp.Description("Optional unified diff to show the change")),
	), siSuggestHandler(cfg))

	s.AddTool(mcp.NewTool("si_list_pending",
		mcp.WithDescription("List pending improvement suggestions awaiting approval."),
		mcp.WithString("status", mcp.Description("Filter by status: pending | approved | rejected | applied (default: pending)")),
	), siListPendingHandler(cfg))

	s.AddTool(mcp.NewTool("si_approve",
		mcp.WithDescription("Approve a pending suggestion (and optionally auto-apply it)."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Suggestion ID")),
		mcp.WithBoolean("apply", mcp.Description("Also apply it immediately (default false)")),
	), siApproveHandler(cfg))

	s.AddTool(mcp.NewTool("si_reject",
		mcp.WithDescription("Reject and discard a pending suggestion."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Suggestion ID")),
		mcp.WithString("reason", mcp.Description("Optional reason")),
	), siRejectHandler(cfg))

	s.AddTool(mcp.NewTool("si_apply",
		mcp.WithDescription("Apply an approved suggestion by running its apply_cmd."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Suggestion ID (must be approved)")),
	), siApplyHandler(cfg))

	s.AddTool(mcp.NewTool("si_report_error",
		mcp.WithDescription("Record an error to CLAUDE_DIR/errors/ for later triage."),
		mcp.WithString("message", mcp.Required(), mcp.Description("Error message or description")),
		mcp.WithString("context", mcp.Description("Additional context (stack trace, command output, etc.)")),
		mcp.WithString("source", mcp.Description("Where the error came from (tool name, service, etc.)")),
	), siReportErrorHandler(cfg))

	s.AddTool(mcp.NewTool("si_tech_digest",
		mcp.WithDescription("Generate a tech digest for a directory and optionally post to Slack/Discord."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Project directory to analyze")),
		mcp.WithBoolean("post_slack", mcp.Description("Post digest to Slack (requires SLACK_TOKEN and channel)")),
		mcp.WithString("slack_channel", mcp.Description("Slack channel to post to")),
		mcp.WithBoolean("post_discord", mcp.Description("Post digest to Discord (requires DISCORD_TOKEN and channel_id)")),
		mcp.WithString("discord_channel_id", mcp.Description("Discord channel ID to post to")),
	), siTechDigestHandler(cfg))
}

func pendingDir(cfg *config.Config) string { return filepath.Join(cfg.ClaudeDir, "pending") }
func errorsDir(cfg *config.Config) string  { return filepath.Join(cfg.ClaudeDir, "errors") }

func loadAllowlist(cfg *config.Config) Allowlist {
	path := filepath.Join(cfg.ClaudeDir, "selfimprove-allowlist.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Allowlist{
			AutoApply:       []string{"format", "lint_fix"},
			RequireApproval: []string{"dependency_update", "refactor", "git_commit"},
		}
	}
	var al Allowlist
	json.Unmarshal(data, &al)
	return al
}

func isAutoApply(al Allowlist, category string) bool {
	for _, c := range al.AutoApply {
		if c == category {
			return true
		}
	}
	return false
}

func saveSuggestion(cfg *config.Config, s PendingSuggestion) error {
	dir := pendingDir(cfg)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, s.ID+".json"), data, 0644)
}

func loadSuggestion(cfg *config.Config, id string) (PendingSuggestion, error) {
	var s PendingSuggestion
	path := filepath.Join(pendingDir(cfg), id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("suggestion %q not found", id)
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

func siScanDirHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ignore := req.GetString("ignore", "")

		var findings []string

		// Git status
		gitOut, _ := exec.Command("git", "-C", dir, "status", "--short").Output()
		if len(gitOut) > 0 {
			findings = append(findings, fmt.Sprintf("=== Git Status ===\n%s", gitOut))
		}

		// Detect tech stack by files
		stack := detectStack(dir)
		findings = append(findings, fmt.Sprintf("=== Detected Stack ===\n%s", strings.Join(stack, "\n")))

		// Count files (respecting .gitignore)
		countArgs := []string{dir, "--count-text"}
		if ignore != "" {
			for _, p := range strings.Split(ignore, ",") {
				countArgs = append(countArgs, "--exclude="+strings.TrimSpace(p))
			}
		}
		fileCount, _ := exec.Command("git", "-C", dir, "ls-files", "--cached", "--others", "--exclude-standard").Output()
		lines := strings.Split(strings.TrimSpace(string(fileCount)), "\n")
		findings = append(findings, fmt.Sprintf("=== Files (tracked+untracked) ===\nCount: %d", len(lines)))

		// Look for common issues
		issues := detectIssues(dir)
		if len(issues) > 0 {
			findings = append(findings, fmt.Sprintf("=== Potential Issues ===\n%s", strings.Join(issues, "\n")))
		}

		return mcp.NewToolResultText(strings.Join(findings, "\n\n")), nil
	}
}

func detectStack(dir string) []string {
	var stack []string
	checks := []struct{ file, label string }{
		{"go.mod", "Go"},
		{"package.json", "Node.js"},
		{"Cargo.toml", "Rust"},
		{"pyproject.toml", "Python"},
		{"requirements.txt", "Python"},
		{"pom.xml", "Java (Maven)"},
		{"build.gradle", "Java/Kotlin (Gradle)"},
		{"Gemfile", "Ruby"},
		{"composer.json", "PHP"},
		{"docker-compose.yml", "Docker Compose"},
		{"Dockerfile", "Docker"},
		{".github/workflows", "GitHub Actions"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			stack = append(stack, "  "+c.label+" ("+c.file+")")
		}
	}
	if len(stack) == 0 {
		stack = append(stack, "  (no recognized stack files found)")
	}
	return stack
}

func detectIssues(dir string) []string {
	var issues []string

	// Check for TODO/FIXME in code
	out, _ := exec.Command("sh", "-c",
		fmt.Sprintf(`git -C %q ls-files | xargs grep -l "TODO\|FIXME\|HACK\|XXX" 2>/dev/null | head -10`, dir),
	).Output()
	if len(strings.TrimSpace(string(out))) > 0 {
		count := len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		issues = append(issues, fmt.Sprintf("  %d file(s) contain TODO/FIXME/HACK markers", count))
	}

	// Check for .env files that might be committed
	envOut, _ := exec.Command("git", "-C", dir, "ls-files", "*.env", ".env").Output()
	if len(strings.TrimSpace(string(envOut))) > 0 {
		issues = append(issues, "  WARNING: .env file(s) appear to be tracked by git")
	}

	return issues
}

func siGitDiffHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		base := req.GetString("base", "")

		var out []byte
		if base != "" {
			out, err = exec.Command("git", "-C", dir, "diff", base).Output()
		} else {
			// staged + unstaged
			staged, _ := exec.Command("git", "-C", dir, "diff", "--cached").Output()
			unstaged, _ := exec.Command("git", "-C", dir, "diff").Output()
			out = append(staged, unstaged...)
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("git diff error: %v", err)), nil
		}
		if len(strings.TrimSpace(string(out))) == 0 {
			return mcp.NewToolResultText("(no changes)"), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}

func siSuggestHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		category, err := req.RequireString("category")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		al := loadAllowlist(cfg)
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		status := "pending"
		if isAutoApply(al, category) {
			status = "approved"
		}

		s := PendingSuggestion{
			ID:          id,
			Category:    category,
			Title:       title,
			Description: description,
			Dir:         dir,
			ApplyCmd:    req.GetString("apply_cmd", ""),
			Diff:        req.GetString("diff", ""),
			CreatedAt:   time.Now(),
			Status:      status,
		}

		if err := saveSuggestion(cfg, s); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save error: %v", err)), nil
		}
		EmitEvent(cfg, Event{
			Type: "suggestion_created",
			ID:   s.ID,
			Data: map[string]any{
				"suggestion_id": s.ID,
				"category":      s.Category,
				"title":         s.Title,
				"status":        s.Status,
				"dir":           s.Dir,
			},
		})

		msg := fmt.Sprintf("suggestion created: %s (id=%s, status=%s)", title, id, status)
		if status == "approved" {
			msg += "\n(auto-approved via allowlist — use si_apply to apply)"
		} else {
			msg += "\n(use si_approve to approve it)"
		}
		return mcp.NewToolResultText(msg), nil
	}
}

func siListPendingHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		statusFilter := req.GetString("status", "pending")

		entries, err := os.ReadDir(pendingDir(cfg))
		if err != nil {
			if os.IsNotExist(err) {
				return mcp.NewToolResultText("(no suggestions)"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("readdir: %v", err)), nil
		}

		var lines []string
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(pendingDir(cfg), e.Name()))
			if err != nil {
				continue
			}
			var s PendingSuggestion
			if err := json.Unmarshal(data, &s); err != nil {
				continue
			}
			if statusFilter != "" && s.Status != statusFilter {
				continue
			}
			lines = append(lines, fmt.Sprintf("[%s] %s — %s (%s)\n  Dir: %s", s.Status, s.ID, s.Title, s.Category, s.Dir))
		}
		if len(lines) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("(no %s suggestions)", statusFilter)), nil
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n\n")), nil
	}
}

func siApproveHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		apply := req.GetBool("apply", false)

		s, err := loadSuggestion(cfg, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		s.Status = "approved"
		if err := saveSuggestion(cfg, s); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save error: %v", err)), nil
		}

		msg := fmt.Sprintf("suggestion %s approved", id)
		if apply && s.ApplyCmd != "" {
			out, err := exec.Command("sh", "-c", s.ApplyCmd).CombinedOutput()
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("apply failed: %v\n%s", err, out)), nil
			}
			s.Status = "applied"
			saveSuggestion(cfg, s)
			msg = fmt.Sprintf("suggestion %s approved and applied\n%s", id, out)
		}
		return mcp.NewToolResultText(msg), nil
	}
}

func siRejectHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		reason := req.GetString("reason", "")

		s, err := loadSuggestion(cfg, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		s.Status = "rejected"
		if reason != "" {
			s.Description = s.Description + "\n\nRejection reason: " + reason
		}
		saveSuggestion(cfg, s)
		return mcp.NewToolResultText(fmt.Sprintf("suggestion %s rejected", id)), nil
	}
}

func siApplyHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		s, err := loadSuggestion(cfg, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if s.Status != "approved" {
			return mcp.NewToolResultError(fmt.Sprintf("suggestion %s is %s, not approved — approve it first with si_approve", id, s.Status)), nil
		}
		if s.ApplyCmd == "" {
			return mcp.NewToolResultError("suggestion has no apply_cmd"), nil
		}

		cmd := exec.Command("sh", "-c", s.ApplyCmd)
		cmd.Dir = s.Dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("apply failed: %v\n%s", err, out)), nil
		}
		s.Status = "applied"
		saveSuggestion(cfg, s)
		return mcp.NewToolResultText(fmt.Sprintf("applied: %s\n%s", s.Title, out)), nil
	}
}

func siReportErrorHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		message, err := req.RequireString("message")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ctx := req.GetString("context", "")
		source := req.GetString("source", "unknown")

		dir := errorsDir(cfg)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("mkdir: %v", err)), nil
		}

		ts := time.Now()
		id := fmt.Sprintf("%d", ts.UnixNano())
		report := map[string]string{
			"id":         id,
			"message":    message,
			"context":    ctx,
			"source":     source,
			"created_at": ts.Format(time.RFC3339),
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		path := filepath.Join(dir, id+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write: %v", err)), nil
		}
		EmitEvent(cfg, Event{
			Type: "error_reported",
			ID:   id,
			Data: map[string]any{
				"id":         id,
				"message":    message,
				"context":    ctx,
				"source":     source,
				"created_at": ts.Format(time.RFC3339),
			},
		})
		return mcp.NewToolResultText(fmt.Sprintf("error recorded (id=%s)\nUse si_suggest to create a fix suggestion.", id)), nil
	}
}

func siTechDigestHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		var parts []string
		parts = append(parts, fmt.Sprintf("Tech Digest — %s\nGenerated: %s", dir, time.Now().Format("2006-01-02 15:04")))

		// Git log (recent commits)
		gitLog, _ := exec.Command("git", "-C", dir, "log", "--oneline", "-10").Output()
		if len(gitLog) > 0 {
			parts = append(parts, "Recent commits:\n"+string(gitLog))
		}

		// Stack
		stack := detectStack(dir)
		parts = append(parts, "Stack:\n"+strings.Join(stack, "\n"))

		// Issues
		issues := detectIssues(dir)
		if len(issues) > 0 {
			parts = append(parts, "Issues:\n"+strings.Join(issues, "\n"))
		}

		// Pending suggestions
		pending := countSuggestionsByStatus(cfg, "pending")
		approved := countSuggestionsByStatus(cfg, "approved")
		parts = append(parts, fmt.Sprintf("Suggestions: %d pending, %d approved (use si_list_pending to review)", pending, approved))

		digest := strings.Join(parts, "\n\n---\n\n")

		// Optionally post to Slack
		if req.GetBool("post_slack", false) {
			channel := req.GetString("slack_channel", "")
			if channel != "" && cfg.SlackToken != "" {
				slackAPICall(cfg, "POST", "chat.postMessage", map[string]any{
					"channel": channel,
					"text":    "```\n" + digest + "\n```",
				})
			}
		}

		// Optionally post to Discord
		if req.GetBool("post_discord", false) {
			channelID := req.GetString("discord_channel_id", "")
			if channelID != "" && cfg.DiscordToken != "" {
				discordDo(cfg, "POST", "/channels/"+channelID+"/messages", map[string]string{
					"content": "```\n" + digest + "\n```",
				})
			}
		}

		return mcp.NewToolResultText(digest), nil
	}
}

func countSuggestionsByStatus(cfg *config.Config, status string) int {
	entries, err := os.ReadDir(pendingDir(cfg))
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pendingDir(cfg), e.Name()))
		if err != nil {
			continue
		}
		var s PendingSuggestion
		if json.Unmarshal(data, &s) == nil && s.Status == status {
			count++
		}
	}
	return count
}

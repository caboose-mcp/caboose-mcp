package tools

// Sandbox — run proposed changes against an immutable copy of a directory.
//
// Flow:
//   1. sandbox_run dir=<path> command=<cmd>
//      - Copies dir to /tmp/caboose-sandbox-<id>/
//      - Runs command inside the copy
//      - Returns: unified diff + file tree of what changed
//      - Sandbox stays on disk for inspection or sandbox_clean
//
//   2. sandbox_suggestion id=<suggestion-id>
//      - Loads a PendingSuggestion, copies its Dir, runs its apply_cmd
//      - Returns diff + "looks good? call si_approve id=<id> apply=true"
//
//   3. sandbox_list   — list active sandboxes with age and size
//   4. sandbox_clean  — delete one or all sandboxes
//   5. sandbox_diff   — re-diff an existing sandbox against current source
//
// What "immutable clone" means here:
//   cp -r source/ sandbox/  — same user, no container.
//   The sandboxed process can still reach the network and other dirs.
//   For full isolation use sandbox_docker (planned: runs in a container via
//   the docker CLI and mounts a copy of the dir read-only).
//   This is sufficient for previewing file-level changes safely.

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

// sandboxMeta is stored as /tmp/caboose-sandbox-<id>/.sandbox.json
type sandboxMeta struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	SourceDir  string    `json:"source_dir"`
	SandboxDir string    `json:"sandbox_dir"`
	Command    string    `json:"command"`
	CreatedAt  time.Time `json:"created_at"`
	Ran        bool      `json:"ran"`
	ExitCode   int       `json:"exit_code"`
	Output     string    `json:"output"`
}

const sandboxPrefix = "caboose-sandbox-"

func RegisterSandbox(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("sandbox_run",
		mcp.WithDescription("Clone a directory to a temp sandbox and run a command inside it. Returns a diff showing what the command changed, without touching the real directory."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Directory to clone and run the command in")),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to run inside the sandbox copy")),
		mcp.WithString("label", mcp.Description("Human-readable label for this sandbox (optional)")),
	), sandboxRunHandler(cfg))

	s.AddTool(mcp.NewTool("sandbox_suggestion",
		mcp.WithDescription("Preview a pending improvement suggestion in a sandbox before approving it. Clones the suggestion's directory and runs its apply_cmd, then shows the diff."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Pending suggestion ID (from si_list_pending)")),
	), sandboxSuggestionHandler(cfg))

	s.AddTool(mcp.NewTool("sandbox_list",
		mcp.WithDescription("List active sandboxes with their age, source directory, and command."),
	), sandboxListHandler(cfg))

	s.AddTool(mcp.NewTool("sandbox_clean",
		mcp.WithDescription("Delete one sandbox by ID, or all sandboxes older than a given age."),
		mcp.WithString("id", mcp.Description("Sandbox ID to delete (omit to delete all)")),
		mcp.WithNumber("older_than_hours", mcp.Description("Delete all sandboxes older than N hours (default: delete all if no id given)")),
	), sandboxCleanHandler(cfg))

	s.AddTool(mcp.NewTool("sandbox_diff",
		mcp.WithDescription("Re-run the diff between an existing sandbox and its original source directory."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Sandbox ID")),
	), sandboxDiffHandler(cfg))
}

// ---- helpers ----

func sandboxDir(id string) string {
	return filepath.Join(os.TempDir(), sandboxPrefix+id)
}

func newSandboxID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func loadSandboxMeta(id string) (sandboxMeta, error) {
	var m sandboxMeta
	data, err := os.ReadFile(filepath.Join(sandboxDir(id), ".sandbox.json"))
	if err != nil {
		return m, fmt.Errorf("sandbox %q not found", id)
	}
	return m, json.Unmarshal(data, &m)
}

func saveSandboxMeta(m sandboxMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.SandboxDir, ".sandbox.json"), data, 0644)
}

// cloneDir copies src into dst using cp -r (preserves permissions, symlinks).
func cloneDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := exec.Command("cp", "-r", src, dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cp error: %v\n%s", err, out)
	}
	return nil
}

// unifiedDiff produces a diff between two directories using `diff -rq` then `diff -ru`.
// Returns the unified diff text.
func unifiedDiff(orig, modified string) string {
	// First get a summary of which files changed
	summaryOut, _ := exec.Command("diff", "-rq",
		"--exclude=.sandbox.json",
		orig, modified,
	).Output()

	if len(strings.TrimSpace(string(summaryOut))) == 0 {
		return "(no changes)"
	}

	// Full unified diff
	diffOut, _ := exec.Command("diff", "-ru",
		"--exclude=.sandbox.json",
		orig, modified,
	).Output()

	result := strings.TrimSpace(string(diffOut))
	if result == "" {
		// Some changes (e.g. new files) only show in -rq not -ru
		result = string(summaryOut)
	}

	// Trim very long diffs
	lines := strings.Split(result, "\n")
	if len(lines) > 200 {
		result = strings.Join(lines[:200], "\n") +
			fmt.Sprintf("\n\n... (%d more lines truncated)", len(lines)-200)
	}
	return result
}

// fileTree returns a compact tree of files under dir (excluding .sandbox.json).
func fileTree(dir string) string {
	out, err := exec.Command("find", dir, "-not", "-name", ".sandbox.json",
		"-not", "-path", "*/.*", "-printf", "%P\n").Output()
	if err != nil {
		// fallback: use ls -la
		out, _ = exec.Command("ls", "-la", dir).Output()
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 50 {
		lines = lines[:50]
		lines = append(lines, fmt.Sprintf("... (%d total)", len(lines)))
	}
	return strings.Join(lines, "\n")
}

// ---- sandbox_run ----

func sandboxRunHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir, err := req.RequireString("dir")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		command, err := req.RequireString("command")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		label := req.GetString("label", "")

		// Resolve absolute path
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bad path: %v", err)), nil
		}
		if _, err := os.Stat(absDir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("directory not found: %s", absDir)), nil
		}

		id := newSandboxID()
		sbDir := sandboxDir(id)

		// Clone
		if err := cloneDir(absDir, sbDir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("clone error: %v", err)), nil
		}

		meta := sandboxMeta{
			ID:         id,
			Label:      label,
			SourceDir:  absDir,
			SandboxDir: sbDir,
			Command:    command,
			CreatedAt:  time.Now(),
		}

		// Run command inside sandbox
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = sbDir
		cmdOut, cmdErr := cmd.CombinedOutput()
		meta.Ran = true
		if cmdErr != nil {
			if exitErr, ok := cmdErr.(*exec.ExitError); ok {
				meta.ExitCode = exitErr.ExitCode()
			} else {
				meta.ExitCode = 1
			}
		}
		meta.Output = strings.TrimSpace(string(cmdOut))
		saveSandboxMeta(meta)

		// Diff sandbox against original
		diff := unifiedDiff(absDir, sbDir)

		var out strings.Builder
		out.WriteString(fmt.Sprintf("Sandbox ID: %s\n", id))
		out.WriteString(fmt.Sprintf("Source:     %s\n", absDir))
		out.WriteString(fmt.Sprintf("Sandbox:    %s\n", sbDir))
		out.WriteString(fmt.Sprintf("Command:    %s\n", command))
		if meta.ExitCode != 0 {
			out.WriteString(fmt.Sprintf("Exit code:  %d (command failed)\n", meta.ExitCode))
		} else {
			out.WriteString("Exit code:  0 (success)\n")
		}
		if meta.Output != "" {
			out.WriteString(fmt.Sprintf("\n--- Command output ---\n%s\n", meta.Output))
		}
		out.WriteString("\n--- Diff (sandbox vs original) ---\n")
		out.WriteString(diff)
		out.WriteString(fmt.Sprintf("\n\nSandbox kept at %s\nRun sandbox_clean id=%s when done.", sbDir, id))

		return mcp.NewToolResultText(out.String()), nil
	}
}

// ---- sandbox_suggestion ----

func sandboxSuggestionHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		s, err := loadSuggestion(cfg, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if s.ApplyCmd == "" {
			return mcp.NewToolResultError("suggestion has no apply_cmd — cannot sandbox"), nil
		}

		// Reuse sandbox_run logic
		sbID := newSandboxID()
		sbDir := sandboxDir(sbID)

		absDir, err := filepath.Abs(s.Dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bad suggestion dir: %v", err)), nil
		}
		if _, err := os.Stat(absDir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("suggestion dir not found: %s", absDir)), nil
		}

		if err := cloneDir(absDir, sbDir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("clone error: %v", err)), nil
		}

		meta := sandboxMeta{
			ID:         sbID,
			Label:      fmt.Sprintf("suggestion:%s", id),
			SourceDir:  absDir,
			SandboxDir: sbDir,
			Command:    s.ApplyCmd,
			CreatedAt:  time.Now(),
		}

		cmd := exec.Command("sh", "-c", s.ApplyCmd)
		cmd.Dir = sbDir
		cmdOut, cmdErr := cmd.CombinedOutput()
		meta.Ran = true
		if cmdErr != nil {
			if exitErr, ok := cmdErr.(*exec.ExitError); ok {
				meta.ExitCode = exitErr.ExitCode()
			}
		}
		meta.Output = strings.TrimSpace(string(cmdOut))
		saveSandboxMeta(meta)

		diff := unifiedDiff(absDir, sbDir)

		var out strings.Builder
		out.WriteString(fmt.Sprintf("Suggestion: [%s] %s\n", s.Status, s.Title))
		out.WriteString(fmt.Sprintf("Category:   %s\n", s.Category))
		out.WriteString(fmt.Sprintf("Apply cmd:  %s\n", s.ApplyCmd))
		out.WriteString(fmt.Sprintf("Sandbox ID: %s\n\n", sbID))

		if meta.ExitCode != 0 {
			out.WriteString(fmt.Sprintf("WARNING: command exited %d\n%s\n\n", meta.ExitCode, meta.Output))
		} else if meta.Output != "" {
			out.WriteString(fmt.Sprintf("Command output:\n%s\n\n", meta.Output))
		}

		out.WriteString("--- Changes this suggestion would make ---\n")
		out.WriteString(diff)

		if diff != "(no changes)" && meta.ExitCode == 0 {
			out.WriteString(fmt.Sprintf("\n\nLooks good? Run:  si_approve id=%s apply=true", id))
		} else if meta.ExitCode != 0 {
			out.WriteString(fmt.Sprintf("\n\nCommand failed in sandbox — review before approving."))
		} else {
			out.WriteString(fmt.Sprintf("\n\nNo file changes detected in sandbox."))
		}

		return mcp.NewToolResultText(out.String()), nil
	}
}

// ---- sandbox_list ----

func sandboxListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries, err := os.ReadDir(os.TempDir())
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("readdir: %v", err)), nil
		}

		var lines []string
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), sandboxPrefix) {
				continue
			}
			id := strings.TrimPrefix(e.Name(), sandboxPrefix)
			meta, err := loadSandboxMeta(id)
			if err != nil {
				lines = append(lines, fmt.Sprintf("  %s (no metadata)", id))
				continue
			}

			age := time.Since(meta.CreatedAt).Round(time.Minute)
			status := "ran"
			if !meta.Ran {
				status = "not run"
			} else if meta.ExitCode != 0 {
				status = fmt.Sprintf("exit %d", meta.ExitCode)
			}

			label := meta.Label
			if label == "" {
				label = meta.Command
				if len(label) > 40 {
					label = label[:40] + "…"
				}
			}
			lines = append(lines, fmt.Sprintf(
				"  %s  [%s]  %s ago  %s → %s",
				id, status, age, meta.SourceDir, label,
			))
		}

		if len(lines) == 0 {
			return mcp.NewToolResultText("(no active sandboxes)"), nil
		}
		return mcp.NewToolResultText("Active sandboxes:\n" + strings.Join(lines, "\n")), nil
	}
}

// ---- sandbox_clean ----

func sandboxCleanHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		specificID := req.GetString("id", "")
		olderThanHours := req.GetFloat("older_than_hours", 0)

		if specificID != "" {
			dir := sandboxDir(specificID)
			if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
				return mcp.NewToolResultError(fmt.Sprintf("remove error: %v", err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("deleted sandbox %s", specificID)), nil
		}

		// Delete all (or all older than threshold)
		entries, _ := os.ReadDir(os.TempDir())
		var deleted, skipped int
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), sandboxPrefix) {
				continue
			}
			id := strings.TrimPrefix(e.Name(), sandboxPrefix)
			dir := sandboxDir(id)

			if olderThanHours > 0 {
				meta, err := loadSandboxMeta(id)
				if err == nil && time.Since(meta.CreatedAt).Hours() < olderThanHours {
					skipped++
					continue
				}
			}
			os.RemoveAll(dir)
			deleted++
		}
		msg := fmt.Sprintf("deleted %d sandbox(es)", deleted)
		if skipped > 0 {
			msg += fmt.Sprintf(", kept %d (newer than %.0fh)", skipped, olderThanHours)
		}
		return mcp.NewToolResultText(msg), nil
	}
}

// ---- sandbox_diff ----

func sandboxDiffHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		meta, err := loadSandboxMeta(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		diff := unifiedDiff(meta.SourceDir, meta.SandboxDir)

		var out strings.Builder
		out.WriteString(fmt.Sprintf("Sandbox: %s\n", id))
		out.WriteString(fmt.Sprintf("Source:  %s\n", meta.SourceDir))
		out.WriteString(fmt.Sprintf("Command: %s\n\n", meta.Command))
		out.WriteString("--- Diff (sandbox vs current source) ---\n")
		out.WriteString(diff)
		return mcp.NewToolResultText(out.String()), nil
	}
}

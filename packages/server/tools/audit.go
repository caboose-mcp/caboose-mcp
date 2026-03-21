package tools

// audit — transparency and gate system for tool execution.
//
// Provides an append-only audit log of every tool invocation (passively written
// by other tools via WriteAuditEntry), plus an optional "gate" mode where
// designated tools must be explicitly approved before running.
//
// Gate flow:
//   1. A gated tool (e.g. execute_command) is called.
//   2. Instead of executing, it calls GateOrRun(). If gating is enabled and
//      the tool is on the gate list, GateOrRun writes a pending file and returns
//      a human-readable "awaiting approval" message.
//   3. The user calls approve_execution(id) — which executes the deferred action
//      and logs the result — or deny_execution(id).
//
// Passive logging:
//   All tools can call WriteAuditEntry() after execution to append a log line.
//   audit_list shows these entries, giving the user a "thinking" view of what ran.
//
// Storage:
//   CLAUDE_DIR/audit/audit.log          — JSONL, one entry per line
//   CLAUDE_DIR/audit/gate-config.json   — GateConfig struct
//   CLAUDE_DIR/audit/pending/<id>.json  — PendingGate structs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ---- types ----

// AuditEntry is one line in audit.log.
type AuditEntry struct {
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"ts"`
	Tool       string    `json:"tool"`
	Params     any       `json:"params,omitempty"`
	Status     string    `json:"status"` // "ok" | "error" | "gated" | "approved" | "denied"
	ResultSnip string    `json:"result_snip,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

// GateConfig controls which tools are gated and whether gating is active.
type GateConfig struct {
	Enabled    bool     `json:"enabled"`
	GatedTools []string `json:"gated_tools"`
}

// PendingGate holds a deferred execution awaiting approval.
type PendingGate struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"ts"`
	Tool      string            `json:"tool"`
	Params    map[string]string `json:"params"`
	Status    string            `json:"status"` // "pending" | "approved" | "denied"
}

// ---- register ----

func RegisterAudit(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("audit_list",
		mcp.WithDescription("Show the audit log of recent tool executions. Use this to see what commands and tools have been run, like a transparent activity feed."),
		mcp.WithNumber("limit", mcp.Description("Number of recent entries to show (default 20, max 200)")),
		mcp.WithString("tool", mcp.Description("Filter by tool name (optional)")),
		mcp.WithString("status", mcp.Description("Filter by status: ok, error, gated, approved, denied (optional)")),
	), auditListHandler(cfg))

	s.AddTool(mcp.NewTool("audit_config",
		mcp.WithDescription("View or modify the audit gate configuration. Controls which tools require approval before executing."),
		mcp.WithString("action", mcp.Required(), mcp.Description("Action: 'get' | 'enable' | 'disable' | 'gate' | 'ungate'")),
		mcp.WithString("tool", mcp.Description("Tool name to gate or ungate (required for 'gate'/'ungate' actions)")),
	), auditConfigHandler(cfg))

	s.AddTool(mcp.NewTool("audit_pending",
		mcp.WithDescription("List tool executions that are waiting for approval (gated and not yet approved or denied)."),
	), auditPendingHandler(cfg))

	s.AddTool(mcp.NewTool("approve_execution",
		mcp.WithDescription("Approve a gated tool execution. The tool will run immediately and its output returned."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Pending gate ID from audit_pending")),
	), approveExecutionHandler(cfg))

	s.AddTool(mcp.NewTool("deny_execution",
		mcp.WithDescription("Deny a gated tool execution. The pending entry is marked denied and discarded."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Pending gate ID from audit_pending")),
	), denyExecutionHandler(cfg))
}

// ---- public helpers used by other tools ----

// WriteAuditEntry appends one entry to audit.log. Silently drops errors to
// avoid breaking the caller — the log is best-effort.
func WriteAuditEntry(cfg *config.Config, entry AuditEntry) {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	dir := filepath.Join(cfg.ClaudeDir, "audit")
	_ = os.MkdirAll(dir, 0700)
	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(entry)
	_, _ = f.Write(append(line, '\n'))
}

// GateOrRun checks whether tool is gated. If gating is disabled or the tool is
// not on the gate list, it runs fn immediately and logs the result.
// If gated, it creates a pending approval entry and returns a "gated" message.
//
// params is a flat map of param names → string representations for display.
// fn is the actual execution function; it returns (output, error).
func GateOrRun(cfg *config.Config, tool string, params map[string]string, fn func() (string, error)) (*mcp.CallToolResult, error) {
	gcfg := loadGateConfig(cfg)
	if gcfg.Enabled && isGated(gcfg, tool) {
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		pg := PendingGate{
			ID:        id,
			Timestamp: time.Now(),
			Tool:      tool,
			Params:    params,
			Status:    "pending",
		}
		if err := savePendingGate(cfg, pg); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create gate entry: %v", err)), nil
		}
		EmitEvent(cfg, Event{
			Type: "gate_fired",
			ID:   id,
			Data: map[string]any{
				"tool":        tool,
				"params":      params,
				"gate_id":     id,
				"approve_cmd": fmt.Sprintf(`approve_execution("%s")`, id),
				"deny_cmd":    fmt.Sprintf(`deny_execution("%s")`, id),
			},
		})
		WriteAuditEntry(cfg, AuditEntry{
			ID: id, Timestamp: pg.Timestamp, Tool: tool, Params: params, Status: "gated",
		})
		msg := fmt.Sprintf("GATED — tool %q requires approval before running.\n\nID: %s\nParams: %s\n\nApprove with: approve_execution(%q)\nDeny with:    deny_execution(%q)",
			tool, id, formatParams(params), id, id)
		return mcp.NewToolResultText(msg), nil
	}

	// Run directly and log.
	start := time.Now()
	out, err := fn()
	dur := time.Since(start).Milliseconds()
	snip := out
	if len(snip) > 200 {
		snip = snip[:200] + "…"
	}
	status := "ok"
	if err != nil {
		status = "error"
		snip = err.Error()
	}
	WriteAuditEntry(cfg, AuditEntry{
		ID: fmt.Sprintf("%d", time.Now().UnixNano()), Timestamp: time.Now(),
		Tool: tool, Params: params, Status: status, ResultSnip: snip, DurationMS: dur,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(out), nil
}

// ---- handlers ----

func auditListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := req.GetInt("limit", 20)
		if limit <= 0 || limit > 200 {
			limit = 20
		}
		toolFilter := req.GetString("tool", "")
		statusFilter := req.GetString("status", "")

		logPath := filepath.Join(cfg.ClaudeDir, "audit", "audit.log")
		data, err := os.ReadFile(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				return mcp.NewToolResultText("(audit log is empty)"), nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}

		var entries []AuditEntry
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var e AuditEntry
			if json.Unmarshal([]byte(line), &e) != nil {
				continue
			}
			if toolFilter != "" && e.Tool != toolFilter {
				continue
			}
			if statusFilter != "" && e.Status != statusFilter {
				continue
			}
			entries = append(entries, e)
		}

		// Return most-recent first.
		sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp.After(entries[j].Timestamp) })
		if len(entries) > limit {
			entries = entries[:limit]
		}

		if len(entries) == 0 {
			return mcp.NewToolResultText("(no matching audit entries)"), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Audit log (%d entries, newest first):\n\n", len(entries)))
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("[%s] %s  tool=%-30s  status=%s  dur=%dms\n",
				e.ID[:10], e.Timestamp.Format("2006-01-02 15:04:05"), e.Tool, e.Status, e.DurationMS))
			if e.ResultSnip != "" {
				sb.WriteString(fmt.Sprintf("  └ %s\n", e.ResultSnip))
			}
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func auditConfigHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, err := req.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		gcfg := loadGateConfig(cfg)

		switch action {
		case "get":
			b, _ := json.MarshalIndent(gcfg, "", "  ")
			return mcp.NewToolResultText(string(b)), nil

		case "enable":
			gcfg.Enabled = true
			if err := saveGateConfig(cfg, gcfg); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("Gate mode enabled. Gated tools will require approval before executing."), nil

		case "disable":
			gcfg.Enabled = false
			if err := saveGateConfig(cfg, gcfg); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("Gate mode disabled. All tools run directly."), nil

		case "gate":
			tool := req.GetString("tool", "")
			if tool == "" {
				return mcp.NewToolResultError("'tool' is required for action 'gate'"), nil
			}
			if !isGated(gcfg, tool) {
				gcfg.GatedTools = append(gcfg.GatedTools, tool)
			}
			if err := saveGateConfig(cfg, gcfg); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Tool %q added to gate list.", tool)), nil

		case "ungate":
			tool := req.GetString("tool", "")
			if tool == "" {
				return mcp.NewToolResultError("'tool' is required for action 'ungate'"), nil
			}
			var filtered []string
			for _, t := range gcfg.GatedTools {
				if t != tool {
					filtered = append(filtered, t)
				}
			}
			gcfg.GatedTools = filtered
			if err := saveGateConfig(cfg, gcfg); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Tool %q removed from gate list.", tool)), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown action %q; use get|enable|disable|gate|ungate", action)), nil
		}
	}
}

func auditPendingHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		gates, err := listPendingGates(cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(gates) == 0 {
			return mcp.NewToolResultText("(no pending approvals)"), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d pending gate(s):\n\n", len(gates)))
		for _, g := range gates {
			sb.WriteString(fmt.Sprintf("ID:   %s\nTool: %s\nTime: %s\nParams:\n",
				g.ID, g.Tool, g.Timestamp.Format("2006-01-02 15:04:05")))
			for k, v := range g.Params {
				sb.WriteString(fmt.Sprintf("  %s = %s\n", k, v))
			}
			sb.WriteString("\n")
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func approveExecutionHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		pg, err := loadPendingGate(cfg, id)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("pending gate not found: %v", err)), nil
		}
		if pg.Status != "pending" {
			return mcp.NewToolResultError(fmt.Sprintf("gate %s is already %s", id, pg.Status)), nil
		}

		// Execute the deferred operation.
		out, execErr := dispatchGatedTool(cfg, pg)
		pg.Status = "approved"
		_ = savePendingGate(cfg, pg)

		status := "approved"
		snip := out
		if execErr != nil {
			status = "error"
			snip = execErr.Error()
		}
		if len(snip) > 200 {
			snip = snip[:200] + "…"
		}
		WriteAuditEntry(cfg, AuditEntry{
			ID: fmt.Sprintf("%d", time.Now().UnixNano()), Timestamp: time.Now(),
			Tool: pg.Tool, Params: pg.Params, Status: status, ResultSnip: snip,
		})

		if execErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("approved but execution failed: %v", execErr)), nil
		}
		return mcp.NewToolResultText(out), nil
	}
}

func denyExecutionHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		pg, err := loadPendingGate(cfg, id)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("pending gate not found: %v", err)), nil
		}
		pg.Status = "denied"
		_ = savePendingGate(cfg, pg)
		WriteAuditEntry(cfg, AuditEntry{
			ID: fmt.Sprintf("%d", time.Now().UnixNano()), Timestamp: time.Now(),
			Tool: pg.Tool, Params: pg.Params, Status: "denied",
		})
		return mcp.NewToolResultText(fmt.Sprintf("Execution of %q (ID: %s) denied.", pg.Tool, id)), nil
	}
}

// ---- gate config persistence ----

func defaultGateConfig() GateConfig {
	return GateConfig{
		Enabled: false,
		GatedTools: []string{
			"execute_command",
			"si_apply",
			"chezmoi_apply",
		},
	}
}

func gateConfigPath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "audit", "gate-config.json")
}

func loadGateConfig(cfg *config.Config) GateConfig {
	data, err := os.ReadFile(gateConfigPath(cfg))
	if err != nil {
		return defaultGateConfig()
	}
	var gc GateConfig
	if json.Unmarshal(data, &gc) != nil {
		return defaultGateConfig()
	}
	return gc
}

func saveGateConfig(cfg *config.Config, gc GateConfig) error {
	dir := filepath.Join(cfg.ClaudeDir, "audit")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(gc, "", "  ")
	return os.WriteFile(gateConfigPath(cfg), b, 0600)
}

func isGated(gc GateConfig, tool string) bool {
	for _, t := range gc.GatedTools {
		if t == tool {
			return true
		}
	}
	return false
}

// ---- pending gate persistence ----

func pendingGatePath(cfg *config.Config, id string) string {
	return filepath.Join(cfg.ClaudeDir, "audit", "pending", id+".json")
}

func savePendingGate(cfg *config.Config, pg PendingGate) error {
	dir := filepath.Join(cfg.ClaudeDir, "audit", "pending")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(pg, "", "  ")
	return os.WriteFile(pendingGatePath(cfg, pg.ID), b, 0600)
}

func loadPendingGate(cfg *config.Config, id string) (PendingGate, error) {
	var pg PendingGate
	data, err := os.ReadFile(pendingGatePath(cfg, id))
	if err != nil {
		return pg, err
	}
	err = json.Unmarshal(data, &pg)
	return pg, err
}

func listPendingGates(cfg *config.Config) ([]PendingGate, error) {
	dir := filepath.Join(cfg.ClaudeDir, "audit", "pending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var gates []PendingGate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		pg, err := loadPendingGate(cfg, id)
		if err != nil || pg.Status != "pending" {
			continue
		}
		gates = append(gates, pg)
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].Timestamp.Before(gates[j].Timestamp) })
	return gates, nil
}

// ---- dispatch for gated tools ----
// When a gated tool is approved, we re-execute it here.
// Currently supports: execute_command.
// Other tools (si_apply, chezmoi_apply) would need their logic extracted;
// for now they print a reminder to call the tool directly.

func dispatchGatedTool(cfg *config.Config, pg PendingGate) (string, error) {
	switch pg.Tool {
	case "execute_command":
		command := pg.Params["command"]
		cwd := pg.Params["cwd"]
		if command == "" {
			return "", fmt.Errorf("missing command in pending gate params")
		}
		cmd := exec.Command("sh", "-c", command)
		if cwd != "" {
			cmd.Dir = cwd
		}
		out, err := cmd.CombinedOutput()
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

	default:
		return fmt.Sprintf("Tool %q was approved. Please now call it directly — caboose-mcp cannot re-dispatch this tool type automatically.", pg.Tool), nil
	}
}

// ---- format helpers ----

func formatParams(params map[string]string) string {
	var parts []string
	for k, v := range params {
		parts = append(parts, fmt.Sprintf("%s=%q", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

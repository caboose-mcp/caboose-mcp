package tools

// health — system health report with ANSI-colored terminal output.
//
// Reads from /proc (CPU, memory) and shell commands (df, uptime, docker, systemctl)
// to produce a formatted dashboard. Color output is enabled by default; pass
// color=false for plain text (e.g. when piping to a file or posting to Slack).
//
// For a live updating view in a terminal:
//   watch -c -n2 'claude --print health_report'
//
// Tools:
//   health_report — CPU load, memory, disk, uptime, systemd services, Docker summary

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterHealth(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("health_report",
		mcp.WithDescription("Generate a system health report: CPU load, memory, disk, uptime, systemd services, and Docker. "+
			"Returns ANSI-colored text by default (pipe through `cat` or set color=false for plain text). "+
			"For a live updating view: `watch -c -n2 'claude --print health_report'`"),
		mcp.WithBoolean("color", mcp.Description("Include ANSI color/bold codes for terminal display (default: true)")),
		mcp.WithArray("services", mcp.WithStringItems(),
			mcp.Description("Extra systemd user services to check beyond the defaults")),
	), healthReportHandler(cfg))
}

// ── ANSI helpers ─────────────────────────────────────────────────────────────

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
	ansiWhite  = "\x1b[97m"
)

type painter struct{ on bool }

func (p painter) bold(s string) string {
	if !p.on {
		return s
	}
	return ansiBold + s + ansiReset
}

func (p painter) col(code, s string) string {
	if !p.on {
		return s
	}
	return code + s + ansiReset
}

func (p painter) dim(s string) string    { return p.col(ansiDim, s) }
func (p painter) green(s string) string  { return p.col(ansiGreen, s) }
func (p painter) red(s string) string    { return p.col(ansiRed, s) }
func (p painter) yellow(s string) string { return p.col(ansiYellow, s) }
func (p painter) cyan(s string) string   { return p.col(ansiCyan, s) }
func (p painter) gray(s string) string   { return p.col(ansiGray, s) }
func (p painter) white(s string) string  { return p.col(ansiWhite, s) }

func (p painter) bar(pct, width int) string {
	filled := (pct * width) / 100
	if filled > width {
		filled = width
	}
	empty := width - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	switch {
	case pct > 90:
		return p.col(ansiRed, bar)
	case pct > 70:
		return p.col(ansiYellow, bar)
	default:
		return p.col(ansiGreen, bar)
	}
}

func (p painter) sep(label string) string {
	dashes := strings.Repeat("─", max(0, 38-len(label)-2))
	return p.bold(label) + "  " + p.dim(dashes)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func run(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).Output()
	return strings.TrimSpace(string(out)), err
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func bytesToHuman(b float64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0fM", b/(1<<20))
	default:
		return fmt.Sprintf("%.0fK", b/1024)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── handler ──────────────────────────────────────────────────────────────────

func healthReportHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		useColor := req.GetBool("color", true)
		extraSvcs := req.GetStringSlice("services", nil)
		p := painter{on: useColor}

		var sb strings.Builder
		wl := func(s string) { sb.WriteString(s + "\n") }

		// ── Header ──────────────────────────────────────────────────────────
		title := "  SYSTEM HEALTH REPORT  "
		ts := time.Now().Format("2006-01-02  15:04:05")
		border := strings.Repeat("═", len(title))
		wl(p.cyan("╔" + border + "╗"))
		wl(p.cyan("║") + p.bold(title) + p.cyan("║"))
		padded := fmt.Sprintf("%*s", len(title), ts)
		wl(p.cyan("║") + p.dim(padded) + p.cyan("║"))
		wl(p.cyan("╚" + border + "╝"))
		wl("")

		// ── Uptime ──────────────────────────────────────────────────────────
		wl(p.sep("UPTIME"))
		if up, err := run("uptime -p 2>/dev/null || uptime"); err == nil {
			wl("  " + up)
		}
		wl("")

		// ── CPU load ────────────────────────────────────────────────────────
		wl(p.sep("CPU LOAD"))
		if load, err := run("cat /proc/loadavg"); err == nil {
			parts := strings.Fields(load)
			if len(parts) >= 3 {
				wl(fmt.Sprintf("  1m: %s  5m: %s  15m: %s",
					p.white(parts[0]), p.white(parts[1]), p.white(parts[2])))
			}
		}
		// CPU usage % via /proc/stat
		if stat1, err := run("head -1 /proc/stat"); err == nil {
			fields := strings.Fields(stat1)
			if len(fields) >= 5 {
				var vals []float64
				for _, f := range fields[1:] {
					vals = append(vals, parseFloat(f))
				}
				idle := vals[3]
				total := 0.0
				for _, v := range vals {
					total += v
				}
				pct := int(100 * (1 - idle/total))
				wl(fmt.Sprintf("  CPU: %s %d%%", p.bar(pct, 20), pct))
			}
		}
		wl("")

		// ── Memory ──────────────────────────────────────────────────────────
		wl(p.sep("MEMORY"))
		if mem, err := run("free -b | awk 'NR==2{print $2,$3,$4}'"); err == nil {
			parts := strings.Fields(mem)
			if len(parts) == 3 {
				total, used, free := parseFloat(parts[0]), parseFloat(parts[1]), parseFloat(parts[2])
				pct := int(100 * used / total)
				wl(fmt.Sprintf("  Total: %-7s  Used: %-7s  Free: %s",
					bytesToHuman(total), p.yellow(bytesToHuman(used)), p.green(bytesToHuman(free))))
				wl(fmt.Sprintf("  %s  %d%%", p.bar(pct, 30), pct))
			}
		}
		wl("")

		// ── Disk ────────────────────────────────────────────────────────────
		wl(p.sep("DISK (/)"))
		if disk, err := run("df -B1 / | awk 'NR==2{print $2,$3,$4}'"); err == nil {
			parts := strings.Fields(disk)
			if len(parts) == 3 {
				total, used, avail := parseFloat(parts[0]), parseFloat(parts[1]), parseFloat(parts[2])
				pct := int(100 * used / total)
				wl(fmt.Sprintf("  Total: %-6s  Used: %-6s  Avail: %s",
					bytesToHuman(total), p.yellow(bytesToHuman(used)), p.green(bytesToHuman(avail))))
				wl(fmt.Sprintf("  %s  %d%%", p.bar(pct, 30), pct))
			}
		}
		wl("")

		// ── Services ────────────────────────────────────────────────────────
		wl(p.sep("SERVICES"))
		defaultSvcs := []string{"devpi-mcp-server"}
		allSvcs := append(defaultSvcs, extraSvcs...)
		for _, svc := range allSvcs {
			state, err := run("systemctl --user is-active " + svc + " 2>/dev/null || echo inactive")
			if err != nil {
				state = "unknown"
			}
			var dot, stateStr string
			switch state {
			case "active":
				dot = p.green("●")
				stateStr = p.green("active")
			case "inactive":
				dot = p.red("○")
				stateStr = p.red("inactive")
			default:
				dot = p.gray("?")
				stateStr = p.gray(state)
			}
			wl(fmt.Sprintf("  %s %-30s %s", dot, svc, stateStr))
		}
		wl("")

		// ── Docker ──────────────────────────────────────────────────────────
		wl(p.sep("DOCKER"))
		running, err1 := run("docker ps -q 2>/dev/null | wc -l")
		total, err2 := run("docker ps -aq 2>/dev/null | wc -l")
		if err1 == nil && err2 == nil {
			wl(fmt.Sprintf("  Running: %s / Total: %s",
				p.green(strings.TrimSpace(running)), strings.TrimSpace(total)))
			// List running container names
			if names, err := run("docker ps --format '{{.Names}}' 2>/dev/null"); err == nil && names != "" {
				for _, name := range strings.Split(names, "\n") {
					wl("    " + p.dim("·") + " " + name)
				}
			}
		} else {
			wl("  " + p.gray("docker not available"))
		}
		wl("")

		return mcp.NewToolResultText(sb.String()), nil
	}
}

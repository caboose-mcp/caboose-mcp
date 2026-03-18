package tools

// focus — ADHD-friendly focus mode.
//
// Lets the developer declare a single active goal, set an optional timer,
// and "park" distractions without losing them. Tools that generate noisy
// background output (source_digest, si_tech_digest, learn nudges) should
// call IsFocused() and bail early with a parked-style note when active.
//
// Storage:
//   CLAUDE_DIR/focus/session.json    — active session (deleted on focus_end)
//   CLAUDE_DIR/focus/parked.md       — accumulated parking lot, append-only
//   CLAUDE_DIR/focus/config.json     — FocusConfig (default duration, etc.)
//
// Tools:
//   focus_start(goal, duration_minutes?)  — begin a focus session
//   focus_status()                        — current goal, time left, parked count
//   focus_park(note)                      — defer a distraction without acting on it
//   focus_end()                           — end session, print summary
//   focus_config(action, ...)             — view/edit defaults

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ---- types ----

type FocusSession struct {
	Goal        string    `json:"goal"`
	StartedAt   time.Time `json:"started_at"`
	DurationMin int       `json:"duration_min"` // 0 = no timer
	Parked      []string  `json:"parked"`
}

type FocusConfig struct {
	DefaultDurationMin int  `json:"default_duration_min"` // 0 = no default timer
	ShowGoalInReplies  bool `json:"show_goal_in_replies"` // prepend [FOCUS: goal] to responses
}

// ---- public helpers ----

// IsFocused returns the active session and true if a focus session is running.
func IsFocused(cfg *config.Config) (FocusSession, bool) {
	s, err := loadFocusSession(cfg)
	if err != nil {
		return FocusSession{}, false
	}
	// If timer set and elapsed, auto-end.
	if s.DurationMin > 0 && time.Since(s.StartedAt) > time.Duration(s.DurationMin)*time.Minute {
		return FocusSession{}, false
	}
	return s, true
}

// FocusGoalPrefix returns "[FOCUS: <goal>] " if a session is active, else "".
func FocusGoalPrefix(cfg *config.Config) string {
	s, ok := IsFocused(cfg)
	fc := loadFocusConfig(cfg)
	if !ok || !fc.ShowGoalInReplies {
		return ""
	}
	return fmt.Sprintf("[FOCUS: %s] ", s.Goal)
}

// ---- register ----

func RegisterFocus(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("focus_start",
		mcp.WithDescription("Start a focus session with a declared goal. Suppresses background noise (digests, learning nudges). Optionally set a timer."),
		mcp.WithString("goal", mcp.Required(), mcp.Description("What you are working on right now, in one sentence")),
		mcp.WithNumber("duration_minutes", mcp.Description("Optional timer in minutes. 0 = no limit (default)")),
	), focusStartHandler(cfg))

	s.AddTool(mcp.NewTool("focus_status",
		mcp.WithDescription("Show the current focus session: goal, time remaining, and number of parked items."),
	), focusStatusHandler(cfg))

	s.AddTool(mcp.NewTool("focus_park",
		mcp.WithDescription("Park a distraction or tangent without acting on it. The note is saved to your parking lot so your brain can let go of it. You stay focused on your current goal."),
		mcp.WithString("note", mcp.Required(), mcp.Description("The thought, idea, or task to defer")),
	), focusParkHandler(cfg))

	s.AddTool(mcp.NewTool("focus_end",
		mcp.WithDescription("End the focus session. Returns a summary: goal, duration, parked items. Clears the active session."),
	), focusEndHandler(cfg))

	s.AddTool(mcp.NewTool("focus_config",
		mcp.WithDescription("View or update focus mode defaults."),
		mcp.WithString("action", mcp.Required(), mcp.Description("'get' | 'set_duration' | 'enable_prefix' | 'disable_prefix'")),
		mcp.WithNumber("duration_minutes", mcp.Description("Default session duration (for 'set_duration'). 0 = no default timer.")),
	), focusConfigHandler(cfg))
}

// ---- handlers ----

func focusStartHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		goal, err := req.RequireString("goal")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		fc := loadFocusConfig(cfg)
		dur := req.GetInt("duration_minutes", fc.DefaultDurationMin)

		session := FocusSession{
			Goal:        goal,
			StartedAt:   time.Now(),
			DurationMin: dur,
			Parked:      []string{},
		}
		if err := saveFocusSession(cfg, session); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		EmitEvent(cfg, Event{
			Type: "focus_started",
			Data: map[string]any{
				"goal":         session.Goal,
				"duration_min": session.DurationMin,
			},
		})

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Focus session started.\n\nGoal: %s\n", goal))
		if dur > 0 {
			sb.WriteString(fmt.Sprintf("Timer: %d minutes (ends ~%s)\n", dur, time.Now().Add(time.Duration(dur)*time.Minute).Format("15:04")))
		} else {
			sb.WriteString("Timer: none (call focus_end when done)\n")
		}
		sb.WriteString("\nBackground tools (digests, learning nudges) are suppressed.\n")
		sb.WriteString("Use focus_park to defer distractions. Use focus_end to wrap up.")
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func focusStatusHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session, ok := IsFocused(cfg)
		if !ok {
			// Check if session exists but timer expired.
			s, err := loadFocusSession(cfg)
			if err == nil && s.DurationMin > 0 {
				return mcp.NewToolResultText(fmt.Sprintf("Focus session for %q has ended (timer elapsed). Call focus_end to see summary.", s.Goal)), nil
			}
			return mcp.NewToolResultText("No active focus session. Use focus_start to begin one."), nil
		}

		elapsed := time.Since(session.StartedAt).Round(time.Second)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Focus: %s\n", session.Goal))
		sb.WriteString(fmt.Sprintf("Elapsed: %s\n", elapsed))
		if session.DurationMin > 0 {
			remaining := time.Duration(session.DurationMin)*time.Minute - elapsed
			if remaining < 0 {
				remaining = 0
			}
			sb.WriteString(fmt.Sprintf("Remaining: %s\n", remaining.Round(time.Second)))
		}
		sb.WriteString(fmt.Sprintf("Parked items: %d\n", len(session.Parked)))
		if len(session.Parked) > 0 {
			sb.WriteString("\nParking lot:\n")
			for i, p := range session.Parked {
				sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, p))
			}
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func focusParkHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		note, err := req.RequireString("note")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Append to parked.md (works even without an active session — parking lot is always available).
		parkedPath := filepath.Join(cfg.ClaudeDir, "focus", "parked.md")
		if mkErr := os.MkdirAll(filepath.Dir(parkedPath), 0700); mkErr != nil {
			return mcp.NewToolResultError(mkErr.Error()), nil
		}
		line := fmt.Sprintf("- [%s] %s\n", time.Now().Format("2006-01-02 15:04"), note)
		f, fErr := os.OpenFile(parkedPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if fErr != nil {
			return mcp.NewToolResultError(fErr.Error()), nil
		}
		_, _ = f.WriteString(line)
		f.Close()

		// Also add to in-memory session if active.
		if session, ok := IsFocused(cfg); ok {
			session.Parked = append(session.Parked, note)
			_ = saveFocusSession(cfg, session)
			return mcp.NewToolResultText(fmt.Sprintf("Parked. Back to: %s", session.Goal)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Parked: %q\n(No active focus session — note saved to parking lot.)", note)), nil
	}
}

func focusEndHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session, err := loadFocusSession(cfg)
		if err != nil {
			return mcp.NewToolResultText("No focus session to end."), nil
		}

		elapsed := time.Since(session.StartedAt).Round(time.Second)
		_ = os.Remove(focusSessionPath(cfg))
		EmitEvent(cfg, Event{
			Type: "focus_ended",
			Data: map[string]any{
				"goal":         session.Goal,
				"elapsed_sec":  int(elapsed.Seconds()),
				"parked_count": len(session.Parked),
			},
		})

		var sb strings.Builder
		sb.WriteString("Focus session ended.\n\n")
		sb.WriteString(fmt.Sprintf("Goal:     %s\n", session.Goal))
		sb.WriteString(fmt.Sprintf("Duration: %s\n", elapsed))
		if len(session.Parked) > 0 {
			sb.WriteString(fmt.Sprintf("\nParked items (%d) — review when ready:\n", len(session.Parked)))
			for i, p := range session.Parked {
				sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, p))
			}
			sb.WriteString(fmt.Sprintf("\nFull parking lot: %s\n", filepath.Join(cfg.ClaudeDir, "focus", "parked.md")))
		} else {
			sb.WriteString("\nNo distractions parked. ")
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func focusConfigHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, err := req.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		fc := loadFocusConfig(cfg)

		switch action {
		case "get":
			b, _ := json.MarshalIndent(fc, "", "  ")
			return mcp.NewToolResultText(string(b)), nil

		case "set_duration":
			fc.DefaultDurationMin = req.GetInt("duration_minutes", 0)
			if err := saveFocusConfig(cfg, fc); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if fc.DefaultDurationMin == 0 {
				return mcp.NewToolResultText("Default duration cleared (no timer by default)."), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Default session duration set to %d minutes.", fc.DefaultDurationMin)), nil

		case "enable_prefix":
			fc.ShowGoalInReplies = true
			if err := saveFocusConfig(cfg, fc); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("Goal prefix enabled: tool replies will include [FOCUS: <goal>] when a session is active."), nil

		case "disable_prefix":
			fc.ShowGoalInReplies = false
			if err := saveFocusConfig(cfg, fc); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("Goal prefix disabled."), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown action %q; use get|set_duration|enable_prefix|disable_prefix", action)), nil
		}
	}
}

// ---- persistence ----

func focusSessionPath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "focus", "session.json")
}

func focusConfigPath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "focus", "config.json")
}

func loadFocusSession(cfg *config.Config) (FocusSession, error) {
	var s FocusSession
	data, err := os.ReadFile(focusSessionPath(cfg))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func saveFocusSession(cfg *config.Config, s FocusSession) error {
	if err := os.MkdirAll(filepath.Dir(focusSessionPath(cfg)), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(focusSessionPath(cfg), b, 0600)
}

func loadFocusConfig(cfg *config.Config) FocusConfig {
	data, err := os.ReadFile(focusConfigPath(cfg))
	if err != nil {
		return FocusConfig{DefaultDurationMin: 0, ShowGoalInReplies: false}
	}
	var fc FocusConfig
	if json.Unmarshal(data, &fc) != nil {
		return FocusConfig{}
	}
	return fc
}

func saveFocusConfig(cfg *config.Config, fc FocusConfig) error {
	if err := os.MkdirAll(filepath.Dir(focusConfigPath(cfg)), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(fc, "", "  ")
	return os.WriteFile(focusConfigPath(cfg), b, 0600)
}

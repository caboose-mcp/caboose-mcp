package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/caboose-mcp/server/tools"
	"github.com/mark3labs/mcp-go/mcp"
)

// sandboxRequest is the JSON body for POST /api/sandbox.
type sandboxRequest struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// sandboxResponse is the JSON body returned by POST /api/sandbox.
type sandboxResponse struct {
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// sandboxAllowlist defines the tools that are callable without authentication.
// These tools are read-only with CORS support via AWS API Gateway proxy.
var sandboxAllowlist = map[string]bool{
	"calendar_today":      true,
	"joke":                true,
	"dad_joke":            true,
	"chuck_norris_joke":   true,
	"mermaid_generate":    true,
}

// ---- Simple in-memory rate limiter ----

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	limit   int
	window  time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		windows: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)

	var recent []time.Time
	for _, t := range rl.windows[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= rl.limit {
		rl.windows[key] = recent
		return false
	}
	rl.windows[key] = append(recent, now)
	return true
}

// sandboxLimiter: 10 requests per minute per IP.
var sandboxLimiter = newRateLimiter(10, time.Minute)

// sandboxHandler returns an http.HandlerFunc for POST /api/sandbox.
// It runs one of the sandboxAllowlist tools in-process and returns the text output.
func sandboxHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Rate limiting by IP
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		if !sandboxLimiter.allow(ip) {
			writeJSON(w, http.StatusTooManyRequests, sandboxResponse{
				Error: "rate limit exceeded — 10 requests/minute per IP",
			})
			return
		}

		var req sandboxRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, sandboxResponse{Error: "invalid JSON body"})
			return
		}

		if !sandboxAllowlist[req.Tool] {
			writeJSON(w, http.StatusForbidden, sandboxResponse{
				Error: "tool not in sandbox allowlist: " + req.Tool,
			})
			return
		}

		start := time.Now()
		output, toolErr := executeSandboxTool(r.Context(), cfg, req.Tool, req.Args)
		elapsed := time.Since(start).Milliseconds()

		if toolErr != nil {
			writeJSON(w, http.StatusOK, sandboxResponse{
				Error:      toolErr.Error(),
				DurationMS: elapsed,
			})
			return
		}

		writeJSON(w, http.StatusOK, sandboxResponse{
			Output:     output,
			DurationMS: elapsed,
		})
	}
}

// executeSandboxTool dispatches to the appropriate tool handler.
func executeSandboxTool(ctx context.Context, cfg *config.Config, toolName string, args map[string]any) (string, error) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args

	var result *mcp.CallToolResult
	var err error

	switch toolName {
	case "calendar_today":
		result, err = calendarTodayForSandbox(ctx, cfg, req)
	case "joke":
		result, err = jokeForSandbox(ctx, cfg, req)
	case "dad_joke":
		result, err = dadJokeForSandbox(ctx, cfg, req)
	case "chuck_norris_joke":
		result, err = chuckNorrisJokeForSandbox(ctx, cfg, req)
	case "mermaid_generate":
		result, err = mermaidForSandbox(ctx, cfg, req)
	default:
		return "", nil
	}

	if err != nil {
		return "", err
	}
	return extractText(result), nil
}

// Thin wrappers that call the tool handler functions.
// These live in the tools package and are invoked here via the handler factory pattern.
func calendarTodayForSandbox(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return tools.CalendarTodayPublic(ctx, cfg, req)
}

func jokeForSandbox(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return tools.JokePublic(ctx, cfg, req)
}

func dadJokeForSandbox(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return tools.DadJokePublic(ctx, cfg, req)
}

func chuckNorrisJokeForSandbox(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return tools.ChuckNorrisJokePublic(ctx, cfg, req)
}

func mermaidForSandbox(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return tools.MermaidPublic(ctx, cfg, req)
}

func extractText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n" + p
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

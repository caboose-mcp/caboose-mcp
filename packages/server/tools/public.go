package tools

// public.go — thin exported wrappers around internal handler factories.
// Used by the sandbox API (sandbox_api.go in the main package) so it can call
// specific tool handlers without going through the MCP server.

import (
	"context"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// CalendarTodayPublic calls the calendar_today handler (no auth, no external services).
func CalendarTodayPublic(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return calendarTodayHandler(cfg)(ctx, req)
}

// JokePublic calls the joke handler (local list, no external services).
func JokePublic(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jokeHandler(cfg)(ctx, req)
}

// DadJokePublic calls the dad_joke handler (local list, no external services).
func DadJokePublic(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return dadJokeHandler(cfg)(ctx, req)
}

// ChuckNorrisJokePublic calls the chuck_norris_joke handler (local list, no external services).
func ChuckNorrisJokePublic(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return newChuckNorrisJokeHandler(cfg, nil, "")(ctx, req)
}

// MermaidPublic calls the mermaid_generate handler (pure text, no external services).
func MermaidPublic(ctx context.Context, cfg *config.Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mermaidGenerateHandler(cfg)(ctx, req)
}

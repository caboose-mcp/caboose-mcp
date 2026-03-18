package tools

// claude — file I/O tools scoped to the Claude config directory (CLAUDE_DIR).
//
// Provides safe, path-traversal-protected read/write/append/list operations
// for files under CLAUDE_DIR (~/.claude by default). Used to manage memory
// files, persona config, notes, and other Claude-adjacent data without
// exposing the full filesystem.
//
// Storage: CLAUDE_DIR/ (all paths are relative to this root)
//
// Tools:
//   claude_read_file     — read a file by relative path within CLAUDE_DIR
//   claude_write_file    — write/overwrite a file within CLAUDE_DIR
//   claude_append_memory — append content to CLAUDE.md or a named memory file
//   claude_list_files    — list files under CLAUDE_DIR or a subdirectory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterClaude(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("claude_read_file",
		mcp.WithDescription("Read a file under the Claude config directory (~/.claude by default). Path is relative to CLAUDE_DIR."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Relative path within CLAUDE_DIR")),
	), claudeReadFileHandler(cfg))

	s.AddTool(mcp.NewTool("claude_write_file",
		mcp.WithDescription("Write or overwrite a file under the Claude config directory. Path is relative to CLAUDE_DIR."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Relative path within CLAUDE_DIR")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write")),
	), claudeWriteFileHandler(cfg))

	s.AddTool(mcp.NewTool("claude_append_memory",
		mcp.WithDescription("Append content to CLAUDE.md or a named memory file under CLAUDE_DIR."),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to append")),
		mcp.WithString("file", mcp.Description("Target filename within CLAUDE_DIR (default: CLAUDE.md)")),
	), claudeAppendMemoryHandler(cfg))

	s.AddTool(mcp.NewTool("claude_list_files",
		mcp.WithDescription("List files under CLAUDE_DIR, optionally within a subdirectory."),
		mcp.WithString("subdir", mcp.Description("Subdirectory within CLAUDE_DIR to list (optional)")),
	), claudeListFilesHandler(cfg))
}

func safePath(claudeDir, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative, not absolute")
	}
	full := filepath.Join(claudeDir, rel)
	rel2, err := filepath.Rel(claudeDir, full)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel2, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}
	return full, nil
}

func claudeReadFileHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		full, err := safePath(cfg.ClaudeDir, path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("read error: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}

func claudeWriteFileHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		content := req.GetString("content", "")
		full, err := safePath(cfg.ClaudeDir, path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("mkdir error: %v", err)), nil
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write error: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("wrote %d bytes to %s", len(content), path)), nil
	}
}

func claudeAppendMemoryHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		file := req.GetString("file", "CLAUDE.md")
		if file == "" {
			file = "CLAUDE.md"
		}
		full, err := safePath(cfg.ClaudeDir, file)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("mkdir error: %v", err)), nil
		}
		f, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("open error: %v", err)), nil
		}
		defer f.Close()
		line := content
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		if _, err := f.WriteString(line); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write error: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("appended to %s", file)), nil
	}
}

func claudeListFilesHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		base := cfg.ClaudeDir
		sub := req.GetString("subdir", "")
		if sub != "" {
			var err error
			base, err = safePath(cfg.ClaudeDir, sub)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("readdir error: %v", err)), nil
		}
		var lines []string
		for _, e := range entries {
			if e.IsDir() {
				lines = append(lines, e.Name()+"/")
			} else {
				lines = append(lines, e.Name())
			}
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}
}

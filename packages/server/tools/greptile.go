package tools

// greptile — AI-powered code search and Q&A via the Greptile API.
//
// Requires GREPTILE_API_KEY. The default repository can be set via
// GREPTILE_REPO (format: "owner/repo"); individual calls can override it.
// Repositories must be indexed via greptile_index before they can be queried.
//
// Tools:
//   greptile_query — ask a natural language question about an indexed codebase
//   greptile_index — trigger indexing of a GitHub repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const greptileBase = "https://api.greptile.com/v2"

func RegisterGreptile(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("greptile_query",
		mcp.WithDescription("Query a repository using the Greptile API for code search and Q&A."),
		mcp.WithString("question", mcp.Required(), mcp.Description("Question about the codebase")),
		mcp.WithString("repo", mcp.Description("Repository in github/owner/repo format (default: GREPTILE_REPO)")),
	), greptileQueryHandler(cfg))

	s.AddTool(mcp.NewTool("greptile_index",
		mcp.WithDescription("Trigger Greptile to index a repository."),
		mcp.WithString("repo", mcp.Description("Repository in github/owner/repo format (default: GREPTILE_REPO)")),
	), greptileIndexHandler(cfg))
}

func greptileGetRepo(cfg *config.Config, req mcp.CallToolRequest) string {
	if r := req.GetString("repo", ""); r != "" {
		return r
	}
	return cfg.GreptileRepo
}

func greptileHTTP(cfg *config.Config, method, path string, body any) ([]byte, error) {
if cfg.GreptileAPIKey == "" {
return nil, fmt.Errorf("Greptile API key not configured")
}
	var reqBody io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, greptileBase+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.GreptileAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("greptile API error %d: %s", resp.StatusCode, data)
	}
	return data, nil
}

func greptileQueryHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		question, err := req.RequireString("question")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		repo := greptileGetRepo(cfg, req)

		payload := map[string]any{
			"messages": []map[string]string{
				{"id": "1", "content": question, "role": "user"},
			},
			"repositories": []map[string]string{
				{"remote": "github", "repository": repo, "branch": "main"},
			},
			"sessionId": "caboose-mcp",
		}

		data, err := greptileHTTP(cfg, "POST", "/query", payload)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result map[string]any
		json.Unmarshal(data, &result)
		if msg, ok := result["message"].(string); ok && msg != "" {
			return mcp.NewToolResultText(msg), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}

func greptileIndexHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repo := greptileGetRepo(cfg, req)

		payload := map[string]any{
			"remote":     "github",
			"repository": repo,
			"branch":     "main",
		}

		data, err := greptileHTTP(cfg, "POST", "/repositories", payload)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}

package tools

import (
	"context"
	"testing"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// extractTextContent pulls the text value from the first TextContent item in a tool result.
func extractTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no TextContent found in result")
	return ""
}

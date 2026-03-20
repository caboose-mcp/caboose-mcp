package tools

import (
	"context"
	"testing"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestChuckNorrisJoke(t *testing.T) {
	cfg := &config.Config{}
	handler := chuckNorrisJokeHandler(cfg)

	// Test without category
	req := mcp.NewCallToolRequest("chuck_norris_joke", map[string]interface{}{})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.Content == nil || len(result.Content) == 0 {
		t.Fatal("handler returned empty result")
	}
	content := result.Content[0].Text
	if content == "" {
		t.Fatal("handler returned empty text")
	}
	t.Logf("Got joke: %s", content)

	// Test with category
	req = mcp.NewCallToolRequest("chuck_norris_joke", map[string]interface{}{
		"category": "career",
	})
	result, err = handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.Content == nil || len(result.Content) == 0 {
		t.Fatal("handler returned empty result")
	}
	content = result.Content[0].Text
	if content == "" {
		t.Fatal("handler returned empty text")
	}
	t.Logf("Got career joke: %s", content)
}

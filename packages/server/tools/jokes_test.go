package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestChuckNorrisJokeHandler_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jokes/random" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		resp := ChuckNorrisJoke{
			Value:      "Chuck Norris can divide by zero.",
			ID:         "test-id",
			URL:        "https://api.chucknorris.io/jokes/test-id",
			Categories: []string{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	handler := newChuckNorrisJokeHandler(&config.Config{}, srv.Client(), srv.URL)
	req := mcp.CallToolRequest{}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractTextContent(t, result)
	// Verify the result contains the joke value and starts with the expected format
	if !strings.Contains(text, "Chuck Norris Joke:") {
		t.Errorf("expected 'Chuck Norris Joke:' prefix in result, got: %q", text)
	}
	if !strings.Contains(text, "Chuck Norris can divide by zero.") {
		t.Errorf("expected joke text in result, got: %q", text)
	}
}

func TestChuckNorrisJokeHandler_WithCategory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cat := r.URL.Query().Get("category")
		if cat != "career" {
			http.Error(w, "expected category=career", http.StatusBadRequest)
			return
		}
		resp := ChuckNorrisJoke{
			Value:      "Chuck Norris's resume lists 'retired' under previous employers.",
			ID:         "test-id-2",
			Categories: []string{"career"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	handler := newChuckNorrisJokeHandler(&config.Config{}, srv.Client(), srv.URL)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"category": "career"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	text := extractTextContent(t, result)
	if !strings.Contains(text, "career") {
		t.Errorf("expected category label in result, got: %q", text)
	}
}

func TestChuckNorrisJokeHandler_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	handler := newChuckNorrisJokeHandler(&config.Config{}, srv.Client(), srv.URL)
	req := mcp.CallToolRequest{}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	// Check for error in content (NewToolResultError puts error text in TextContent)
	text := extractTextContent(t, result)
	if !strings.Contains(text, "API error") {
		t.Errorf("expected error message for API error, got: %q", text)
	}
}

func TestChuckNorrisJokeHandler_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	handler := newChuckNorrisJokeHandler(&config.Config{}, srv.Client(), srv.URL)
	req := mcp.CallToolRequest{}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	// Check for error message in content (NewToolResultError puts error text in TextContent)
	text := extractTextContent(t, result)
	if !strings.Contains(text, "Error parsing joke") {
		t.Errorf("expected error message for invalid JSON, got: %q", text)
	}
}

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

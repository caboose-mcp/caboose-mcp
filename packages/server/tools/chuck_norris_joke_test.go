package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// contentText extracts the text from the first content item of a tool result.
func contentText(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestChuckNorrisJoke(t *testing.T) {
	// Set up a fake server that returns a predictable joke
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		joke := ChuckNorrisJoke{
			ID:    "test-id",
			Value: "Chuck Norris can unit test entire applications with a single assert.",
			URL:   "https://api.chucknorris.io/jokes/test-id",
		}
		category := r.URL.Query().Get("category")
		if category != "" {
			joke.Category = category
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(joke)
	}))
	defer srv.Close()

	handler := chuckNorrisJokeHandler(srv.Client(), srv.URL)

	t.Run("without category", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "chuck_norris_joke"
		req.Params.Arguments = map[string]interface{}{}
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		text := contentText(result)
		if !strings.Contains(text, "Chuck Norris Joke:") {
			t.Errorf("unexpected result: %s", text)
		}
		if !strings.Contains(text, "can unit test") {
			t.Errorf("joke text missing from result: %s", text)
		}
	})

	t.Run("with category", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Name = "chuck_norris_joke"
		req.Params.Arguments = map[string]interface{}{"category": "career"}
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		text := contentText(result)
		if !strings.Contains(text, "Chuck Norris Joke (career):") {
			t.Errorf("expected category in result, got: %s", text)
		}
	})

	t.Run("api error response", func(t *testing.T) {
		errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer errSrv.Close()

		errHandler := chuckNorrisJokeHandler(errSrv.Client(), errSrv.URL)
		req := mcp.CallToolRequest{}
		req.Params.Name = "chuck_norris_joke"
		req.Params.Arguments = map[string]interface{}{}
		result, err := errHandler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		text := contentText(result)
		if !strings.Contains(text, "API error (status 404)") {
			t.Errorf("expected API error in result, got: %s", text)
		}
	})
}

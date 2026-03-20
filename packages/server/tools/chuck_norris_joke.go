package tools

// chuck_norris_joke — fetch random Chuck Norris jokes from api.chucknorris.io
//
// Fetches jokes from the external Chuck Norris API.
// Supports optional category filtering.
//
// Tools:
//   chuck_norris_joke — fetch a random Chuck Norris joke

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type ChuckNorrisJoke struct {
	Value      string   `json:"value"`
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Categories []string `json:"categories"`
}

func RegisterChuckNorrisJoke(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("chuck_norris_joke",
		mcp.WithDescription("Fetch a random Chuck Norris joke from the api.chucknorris.io API"),
		mcp.WithString("category", mcp.Description("Optional category of joke (e.g. 'career', 'celebrity', 'explicit'). If not specified, returns a random joke.")),
	), chuckNorrisJokeHandler(cfg))
}

func chuckNorrisJokeHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		category := req.GetString("category", "")

		// Build the API URL
		apiURL := "https://api.chucknorris.io/jokes/random"
		if category != "" {
			// URL encode the category parameter
			apiURL = fmt.Sprintf("https://api.chucknorris.io/jokes/random?category=%s", url.QueryEscape(category))
		}

		// Make the HTTP request
		resp, err := http.Get(apiURL)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error fetching joke: %v", err)), nil
		}
		defer resp.Body.Close()

		// Check response status
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return mcp.NewToolResultError(fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(body))), nil
		}

		// Read the response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error reading response: %v", err)), nil
		}

		// Parse the JSON response
		var joke ChuckNorrisJoke
		if err := json.Unmarshal(body, &joke); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error parsing joke: %v", err)), nil
		}

		// Return the joke
		result := fmt.Sprintf("Chuck Norris Joke:\n\n%s", joke.Value)
		if len(joke.Categories) > 0 {
			result = fmt.Sprintf("Chuck Norris Joke (%s):\n\n%s", joke.Categories[0], joke.Value)
		}

		return mcp.NewToolResultText(result), nil
	}
}

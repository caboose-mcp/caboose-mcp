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
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const chuckNorrisBaseURL = "https://api.chucknorris.io"

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
	), chuckNorrisJokeHandler(http.DefaultClient, chuckNorrisBaseURL))
}

<<<<<<< HEAD
func chuckNorrisJokeHandler(client *http.Client, baseURL string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
=======
var chuckNorrisHTTPClient = &http.Client{Timeout: 10 * time.Second}

func chuckNorrisJokeHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
>>>>>>> origin/main
		category := req.GetString("category", "")

		// Build the API URL
		apiURL := baseURL + "/jokes/random"
		if category != "" {
			apiURL = fmt.Sprintf("%s/jokes/random?category=%s", baseURL, url.QueryEscape(category))
		}

<<<<<<< HEAD
		// Make the HTTP request
		resp, err := client.Get(apiURL)
=======
		// Make the HTTP request using the caller's context so it can be cancelled
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return mcp.NewToolResultText(fmt.Sprintf("Error creating request: %v", err)), nil
		}
		resp, err := chuckNorrisHTTPClient.Do(httpReq)
>>>>>>> origin/main
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
			result = fmt.Sprintf("Chuck Norris Joke (%s):\n\n%s", strings.Join(joke.Categories, ", "), joke.Value)
		}

		return mcp.NewToolResultText(result), nil
	}
}

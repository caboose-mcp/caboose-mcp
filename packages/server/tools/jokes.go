package tools

// jokes — random joke dispensary for programming, dad jokes, and Chuck Norris facts.
//
// Includes hardcoded jokes (offline) and Chuck Norris API integration (external API).
//
// Tools:
//   joke            — tell a random programming or nerdy joke
//   dad_joke        — tell a random dad joke (groaning is mandatory)
//   chuck_norris_joke — fetch a random Chuck Norris joke from api.chucknorris.io

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterJokes(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("joke",
		mcp.WithDescription("Tell a random programming or nerdy joke."),
	), jokeHandler(cfg))

	s.AddTool(mcp.NewTool("dad_joke",
		mcp.WithDescription("Tell a random dad joke. Groaning is mandatory."),
	), dadJokeHandler(cfg))

	s.AddTool(mcp.NewTool("chuck_norris_joke",
		mcp.WithDescription("Fetch a random Chuck Norris joke from the api.chucknorris.io API"),
		mcp.WithString("category", mcp.Description("Optional category of joke (e.g. 'career', 'celebrity', 'explicit'). If not specified, returns a random joke.")),
	), newChuckNorrisJokeHandler(cfg, nil, ""))
}

func jokeHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jokes := []string{
			"Why do programmers prefer dark mode? Because light attracts bugs.",
			"A SQL query walks into a bar and asks two tables: 'Can I join you?'",
			"There are only 10 types of people: those who understand binary, and those who don't.",
			"Why do Java developers wear glasses? Because they don't C#.",
			"A programmer's wife says: 'Get a gallon of milk, and if they have eggs, get a dozen.' He returns with 12 gallons of milk.",
			"Knock knock. Race condition. Who's there?",
			"Why did the developer go broke? Because he used up all his cache.",
			"Real programmers count from 0.",
		}
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		return mcp.NewToolResultText(jokes[r.Intn(len(jokes))]), nil
	}
}

func dadJokeHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jokes := []string{
			"I'm reading a book on anti-gravity. It's impossible to put down.",
			"Did you hear about the guy who invented Lifesavers? He made a mint.",
			"Why can't you give Elsa a balloon? Because she'll let it go.",
			"I used to hate facial hair, but then it grew on me.",
			"Why don't scientists trust atoms? Because they make up everything.",
			"I told my wife she was drawing her eyebrows too high. She looked surprised.",
			"What do you call cheese that isn't yours? Nacho cheese.",
			"I asked my dog what two minus two is. He said nothing.",
			"Why do cows wear bells? Because their horns don't work.",
			"I used to be addicted to soap. I'm clean now.",
			"Did you hear about the claustrophobic astronaut? He just needed a little space.",
			"Why did the scarecrow win an award? Because he was outstanding in his field.",
			"I'm on a seafood diet. I see food and I eat it.",
			"What do you call a fake noodle? An impasta.",
			"How do you organize a space party? You planet.",
		}
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		return mcp.NewToolResultText(jokes[r.Intn(len(jokes))]), nil
	}
}

type ChuckNorrisJoke struct {
	Value      string   `json:"value"`
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Categories []string `json:"categories"`
}

// newChuckNorrisJokeHandler returns a handler for the chuck_norris_joke tool.
// httpClient and baseURL can be overridden for testing (pass nil/"" for production defaults).
func newChuckNorrisJokeHandler(cfg *config.Config, httpClient *http.Client, baseURL string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api.chucknorris.io"
	}
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		category := req.GetString("category", "")

		// Build the API URL
		apiURL := baseURL + "/jokes/random"
		if category != "" {
			// URL encode the category parameter
			apiURL = fmt.Sprintf("%s/jokes/random?category=%s", baseURL, url.QueryEscape(category))
		}

		// Make the HTTP request using context with timeout
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error building request: %v", err)), nil
		}
		resp, err := httpClient.Do(httpReq)
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

package tools

// jokes — random joke dispensary for programming, dad jokes, and Chuck Norris facts.
//
// All jokes are hardcoded (offline) — no external API calls.
//
// Tools:
//   joke            — tell a random programming or nerdy joke
//   dad_joke        — tell a random dad joke (groaning is mandatory)
//   chuck_norris_joke — tell a random Chuck Norris joke

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
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

// newChuckNorrisJokeHandler returns a handler for the chuck_norris_joke tool.
// httpClient and baseURL parameters are unused but kept for backward compatibility with testing.
func newChuckNorrisJokeHandler(cfg *config.Config, httpClient *http.Client, baseURL string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Hardcoded Chuck Norris jokes (no external API call)
	chuckNorrisJokes := map[string][]string{
		"dev":        {"Chuck Norris doesn't code in cycles, he codes in strikes.", "Chuck Norris finished World of Warcraft.", "All programming languages were created by Chuck Norris. All other languages are just rip-offs."},
		"career":     {"Chuck Norris never gets a job because it would be a waste of his talents.", "Chuck Norris is the only person who doesn't need a job."},
		"celebrity":  {"Chuck Norris is not a celebrity. Celebrities are not Chuck Norris.", "The only celebrity Chuck Norris respects is himself."},
		"explicit":   {"Chuck Norris does not need the internet because Chuck Norris is the internet.", "They say money can't buy happiness. But it can buy a Chuck Norris action figure. Instant happiness."},
	}

	defaultJokes := []string{
		"Chuck Norris does not need to type-cast. The Chuck-Norris Compiler (CNC) sees through things. Always.",
		"When a bug sees Chuck Norris, it flees screaming in terror, and the compiler catches it.",
		"Chuck Norris doesn't pair program.",
		"Every SQL statement that Chuck Norris codes has an implicit 'COMMIT' in its end.",
		"Chuck Norris rewrote the Google search engine from scratch.",
		"Chuck Norris solved the halting problem.",
		"Chuck Norris instantiates abstract classes.",
		"The only pattern Chuck Norris knows is God Object.",
		"Chuck Norris doesn't need the internet because Chuck Norris is the internet.",
		"Chuck Norris breaks RSA 128-bit encrypted codes in milliseconds.",
	}

	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		category := req.GetString("category", "")

		// Select jokes based on category
		jokes := defaultJokes
		if category != "" {
			if categoryJokes, ok := chuckNorrisJokes[strings.ToLower(category)]; ok {
				jokes = categoryJokes
			}
		}

		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		selectedJoke := jokes[r.Intn(len(jokes))]

		result := fmt.Sprintf("Chuck Norris Joke:\n\n%s", selectedJoke)
		if category != "" {
			result = fmt.Sprintf("Chuck Norris Joke (%s):\n\n%s", category, selectedJoke)
		}

		return mcp.NewToolResultText(result), nil
	}
}

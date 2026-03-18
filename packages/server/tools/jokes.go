package tools

// jokes — random joke dispensary for programming and dad jokes.
//
// Jokes are hardcoded slices selected at random using a time-seeded source.
// No external API calls; fully offline.
//
// Tools:
//   joke     — tell a random programming or nerdy joke
//   dad_joke — tell a random dad joke (groaning is mandatory)

import (
	"context"
	"math/rand"
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

package tools

// jokes — random joke dispensary for programming and dad jokes.
//
// joke and dad_joke use hardcoded local lists (offline, no external dependencies).
//
// Tools:
//   joke     — tell a random programming or nerdy joke (hardcoded)
//   dad_joke — tell a random dad joke (hardcoded, groaning is mandatory)

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"

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
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(jokes))))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error generating random number: %v", err)), nil
		}
		return mcp.NewToolResultText(jokes[n.Int64()]), nil
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
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(jokes))))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error generating random number: %v", err)), nil
		}
		return mcp.NewToolResultText(jokes[n.Int64()]), nil
	}
}


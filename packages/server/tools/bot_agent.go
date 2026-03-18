package tools

// bot_agent — shared Claude agent loop for chat bots.
//
// Provides the ChatProvider interface and RunBotAgent helper so that
// platform-specific bot implementations (Slack, Discord, …) share a single
// Anthropic API call path.
//
// Required cfg fields:
//
//	AnthropicAPIKey — ANTHROPIC_API_KEY

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/caboose-mcp/server/config"
)

// ChatProvider is implemented by each chat platform's bot driver to provide
// platform-specific context to the shared agent loop.
type ChatProvider interface {
	// Name returns the platform name used in log/error messages (e.g. "slack").
	Name() string
}

// RunBotAgent sends userText to Claude and returns the assistant's reply.
// It uses cfg.AnthropicAPIKey for authentication.
func RunBotAgent(ctx context.Context, cfg *config.Config, provider ChatProvider, userText string) (string, error) {
	client := anthropic.NewClient(option.WithAPIKey(cfg.AnthropicAPIKey))

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5,
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userText)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("%s bot: %w", provider.Name(), err)
	}

	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String(), nil
}

// splitMessage splits text into chunks of at most maxLen bytes, preferring to
// break on newline boundaries when possible to keep chunks readable.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	for len(text) > maxLen {
		cut := maxLen
		// prefer breaking at a newline in the last quarter of the window
		if idx := strings.LastIndexByte(text[:cut], '\n'); idx > maxLen*3/4 {
			cut = idx + 1
		}
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	if len(text) > 0 {
		chunks = append(chunks, text)
	}
	return chunks
}

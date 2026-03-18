package tools

// discord_gateway — Discord bot gateway using the ChatProvider abstraction.
//
// Connects to Discord's WebSocket gateway and listens for messages in
// configured channels and DMs. Processes messages through the shared
// Claude agent loop (bot_agent.go) and posts formatted replies.
//
// Required env vars:
//   DISCORD_TOKEN        — bot token
//   ANTHROPIC_API_KEY    — Claude API key
//
// Optional env vars:
//   DISCORD_BOT_CHANNELS — comma-separated channel IDs to listen in
//                          (leave empty to respond only in DMs)

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/caboose-mcp/server/config"
)

// RunDiscordBot starts the Discord gateway bot and blocks until the context
// is cancelled or a fatal error occurs.
func RunDiscordBot(cfg *config.Config) error {
	if cfg.DiscordToken == "" {
		return fmt.Errorf("DISCORD_TOKEN is not set")
	}
	if cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	provider := DiscordProvider{}

	// Build allowed channel set from config
	allowedChannels := map[string]bool{}
	for _, ch := range strings.Split(cfg.DiscordBotChannels, ",") {
		ch = strings.TrimSpace(ch)
		if ch != "" {
			allowedChannels[ch] = true
		}
	}

	dg, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return fmt.Errorf("creating discord session: %w", err)
	}

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore messages from self
		if m.Author.ID == s.State.User.ID {
			return
		}

		isDM := m.GuildID == ""
		inAllowedChannel := allowedChannels[m.ChannelID]

		// Respond in DMs always; in guilds only if channel is allowed
		if !isDM && !inAllowedChannel {
			return
		}

		// Must mention the bot or be a DM to trigger a response
		botMentioned := false
		for _, u := range m.Mentions {
			if u.ID == s.State.User.ID {
				botMentioned = true
				break
			}
		}
		if !isDM && !botMentioned {
			return
		}

		// Strip the @mention prefix from the message
		content := strings.TrimSpace(m.Content)
		if s.State.User != nil {
			content = strings.ReplaceAll(content, "<@"+s.State.User.ID+">", "")
			content = strings.ReplaceAll(content, "<@!"+s.State.User.ID+">", "")
			content = strings.TrimSpace(content)
		}
		if content == "" {
			return
		}

		// Show typing indicator while processing
		s.ChannelTyping(m.ChannelID)

		reply, err := RunBotAgent(context.Background(), cfg, provider, content)
		if err != nil {
			log.Printf("discord bot agent error: %v", err)
			s.ChannelMessageSend(m.ChannelID, "⚠️ *The ravens returned with troubling news:* `"+err.Error()+"`")
			return
		}

		// Discord messages are capped at 2000 chars — split if needed
		for _, chunk := range splitMessage(reply, 2000) {
			s.ChannelMessageSend(m.ChannelID, chunk)
		}
	})

	if err := dg.Open(); err != nil {
		return fmt.Errorf("opening discord connection: %w", err)
	}
	defer dg.Close()

	log.Printf("⚔️  Caboose of the Shire is now online in Discord (channels: %v, DMs: always)", cfg.DiscordBotChannels)

	// Block forever — caller should handle OS signals if needed
	select {}
}

// splitMessage splits a long message into chunks of at most maxLen characters,
// breaking on newlines where possible.
func splitMessage(msg string, maxLen int) []string {
	if len(msg) <= maxLen {
		return []string{msg}
	}
	var chunks []string
	for len(msg) > maxLen {
		cut := maxLen
		// Try to break on a newline
		if idx := strings.LastIndex(msg[:maxLen], "\n"); idx > 0 {
			cut = idx + 1
		}
		chunks = append(chunks, msg[:cut])
		msg = msg[cut:]
	}
	if msg != "" {
		chunks = append(chunks, msg)
	}
	return chunks
}

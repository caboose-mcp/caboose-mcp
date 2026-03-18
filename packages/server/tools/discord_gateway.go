package tools

// discord_gateway — Discord gateway bot using the ChatProvider abstraction.
//
// Connects to Discord and listens for DMs and optionally configured channel
// messages. Processes messages through the shared Claude agent loop
// (bot_agent.go) and posts formatted replies.
//
// Required env vars:
//
//	DISCORD_TOKEN     — Bot token
//	ANTHROPIC_API_KEY — Claude API key
//
// Optional env vars:
//
//	DISCORD_BOT_CHANNELS — comma-separated channel IDs to also respond in
//	                       (DMs always work without this)

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/caboose-mcp/server/config"
)

// discordProvider implements ChatProvider for the Discord gateway bot.
type discordProvider struct{}

func (discordProvider) Name() string { return "discord" }

// RunDiscordBot starts the Discord gateway bot and blocks until interrupted.
func RunDiscordBot(cfg *config.Config) error {
	if cfg.DiscordToken == "" {
		return fmt.Errorf("DISCORD_TOKEN is not set")
	}
	if cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	provider := discordProvider{}

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

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot {
			return
		}

		isDM := m.GuildID == ""
		inAllowedChannel := allowedChannels[m.ChannelID]
		if !isDM && !inAllowedChannel {
			return
		}

		text := strings.TrimSpace(m.Content)
		if text == "" {
			return
		}

		reply, err := RunBotAgent(context.Background(), cfg, provider, text)
		if err != nil {
			log.Printf("discord bot agent error: %v", err)
			s.ChannelMessageSend(m.ChannelID, "error: `"+err.Error()+"`") //nolint:errcheck
			return
		}

		for _, chunk := range splitMessage(reply, 2000) {
			s.ChannelMessageSend(m.ChannelID, chunk) //nolint:errcheck
		}
	})

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	if err := dg.Open(); err != nil {
		return fmt.Errorf("opening discord connection: %w", err)
	}
	defer dg.Close()

	log.Printf("Caboose-MCP Discord bot is online (channels: %q, DMs: always)", cfg.DiscordBotChannels)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

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
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/caboose-mcp/server/config"
)

// DiscordSender implements PlatformSender for Discord.
type DiscordSender struct {
	s     *discordgo.Session
	cache *botMsgCache // optional cache for 🔊 reaction TTS; nil if caching disabled
}

func (d DiscordSender) SendText(channelID, text string) (string, error) {
	msg, err := d.s.ChannelMessageSend(channelID, text)
	if err != nil {
		return "", err
	}
	return msg.ID, nil
}

func (d DiscordSender) SendAudio(channelID string, audio []byte) error {
	_, err := d.s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Files: []*discordgo.File{{Name: "caboose.mp3", Reader: bytes.NewReader(audio)}},
	})
	return err
}

func (d DiscordSender) SendTyping(channelID string) {
	d.s.ChannelTyping(channelID)
}

func (d DiscordSender) MaxMessageLen() int {
	return 1800 // Discord's limit is 2000, we reserve 200 for safety
}

func (d DiscordSender) StartThread(channelID, messageID, name string) (string, error) {
	t, err := d.s.MessageThreadStartComplex(channelID, messageID, &discordgo.ThreadStart{
		Name:                name,
		AutoArchiveDuration: 60,
	})
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

// SendTextInThread posts a message into a Discord thread.
// In Discord, threads are channels, so we post directly to threadID.
func (d DiscordSender) SendTextInThread(channelID, threadID, text string) (string, error) {
	return d.SendText(threadID, text)
}

// CacheReply implements MessageCacher — stores the full reply keyed by message ID
// so the 🔊 reaction handler can speak the full text rather than just the visible chunk.
func (d DiscordSender) CacheReply(msgID, fullReply string) {
	if d.cache != nil {
		d.cache.set(msgID, fullReply)
	}
}

// botMsgCache is a size-capped map of bot message ID → reply text for 🔊 reactions.
type botMsgCache struct {
	mu   sync.Mutex
	keys []string
	data map[string]string
}

func newBotMsgCache() *botMsgCache {
	return &botMsgCache{data: make(map[string]string)}
}

func (c *botMsgCache) set(id, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.data[id]; !exists {
		c.keys = append(c.keys, id)
		if len(c.keys) > 100 {
			delete(c.data, c.keys[0])
			c.keys = c.keys[1:]
		}
	}
	c.data[id] = text
}

func (c *botMsgCache) get(id string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[id]
	return v, ok
}

// RunDiscordBot starts the Discord gateway bot and blocks until the context
// is cancelled or a fatal error occurs.
func RunDiscordBot(ctx context.Context, cfg *config.Config) error {
	if cfg.DiscordToken == "" {
		return fmt.Errorf("DISCORD_TOKEN is not set")
	}
	if cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

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

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent | discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsDirectMessageReactions

	cache := newBotMsgCache()

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		msg, ok := parseDiscordMessage(s, m, allowedChannels)
		if !ok {
			return
		}

		// Create sender (with cache) and provider for this request
		sender := DiscordSender{s: s, cache: cache}
		provider := DiscordProvider{sender: sender}

		if !EnqueueBotMessage(context.Background(), cfg, msg, sender, provider) {
			s.ChannelMessageSend(m.ChannelID, "⚔️ *I'm mid-battle — try again in a moment.*")
		}
	})

	// 🔊 reaction handler — synthesize audio on demand for any bot message
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		// Ignore bots and self
		if r.Member != nil && r.Member.User != nil && r.Member.User.Bot {
			return
		}
		if s.State != nil && s.State.User != nil && r.UserID == s.State.User.ID {
			return
		}
		if r.Emoji.Name != "🔊" {
			return
		}

		// Look up cached text or fetch the message directly
		text, found := cache.get(r.MessageID)
		if !found {
			msg, err := s.ChannelMessage(r.ChannelID, r.MessageID)
			if err != nil {
				log.Printf("discord reaction: fetch message %s: %v", r.MessageID, err)
				return
			}
			text = msg.Content
		}
		if text == "" {
			return
		}

		audio, err := Synthesize(context.Background(), cfg, text)
		if err != nil {
			log.Printf("discord reaction tts error: %v", err)
			return
		}
		if audio == nil {
			return
		}

		s.ChannelMessageSendComplex(r.ChannelID, &discordgo.MessageSend{
			Files: []*discordgo.File{{Name: "caboose.mp3", Reader: bytes.NewReader(audio)}},
		})
	})

	if err := dg.Open(); err != nil {
		return fmt.Errorf("opening discord connection: %w", err)
	}
	defer dg.Close()

	log.Printf("⚔️  Caboose of the Shire is now online in Discord (channels: %v, DMs: always)", cfg.DiscordBotChannels)

	// Block until the provided context is cancelled, then shut down gracefully.
	<-ctx.Done()
	log.Printf("Discord bot shutting down: %v", ctx.Err())
	return nil
}

// parseDiscordMessage extracts and validates an IncomingMessage from a Discord message.
// Returns (msg, false) if the message should be ignored (bot, empty, etc).
func parseDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate, allowedChannels map[string]bool) (IncomingMessage, bool) {
	// Ignore messages from other bots (including ourselves)
	if m.Author != nil && m.Author.Bot {
		return IncomingMessage{}, false
	}

	// Ignore messages from self when state information is available
	if s.State != nil && s.State.User != nil && m.Author != nil && m.Author.ID == s.State.User.ID {
		return IncomingMessage{}, false
	}

	isDM := m.GuildID == ""
	inAllowedChannel := allowedChannels[m.ChannelID]

	// Respond in DMs always; in guilds only if channel is allowed
	if !isDM && !inAllowedChannel {
		return IncomingMessage{}, false
	}

	// Must mention the bot or be a DM to trigger a response
	botMentioned := false
	for _, u := range m.Mentions {
		if s.State != nil && s.State.User != nil && u.ID == s.State.User.ID {
			botMentioned = true
			break
		}
	}
	if !isDM && !botMentioned {
		return IncomingMessage{}, false
	}

	// Strip the @mention prefix from the message
	content := strings.TrimSpace(m.Content)
	if s.State != nil && s.State.User != nil {
		content = strings.ReplaceAll(content, "<@"+s.State.User.ID+">", "")
		content = strings.ReplaceAll(content, "<@!"+s.State.User.ID+">", "")
		content = strings.TrimSpace(content)
	}
	if content == "" {
		return IncomingMessage{}, false
	}

	return IncomingMessage{
		UserKey:           "discord:" + m.Author.ID,
		ChannelID:         m.ChannelID,
		OriginalMessageID: m.ID,
		Content:           content,
		IsDM:              isDM,
	}, true
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

package tools

// slack_gateway — Slack Socket Mode bot using the ChatProvider abstraction.
//
// Connects to Slack via Socket Mode (no public URL required) and listens for
// DMs and optionally configured channel messages. Processes messages through
// the shared Claude agent loop (bot_agent.go) and posts formatted replies.
//
// Required env vars:
//
//	SLACK_TOKEN     — Bot OAuth token (xoxb-...)
//	SLACK_APP_TOKEN — App-Level token (xapp-...) with connections:write scope
//	ANTHROPIC_API_KEY — Claude API key
//
// Optional env vars:
//
//	SLACK_BOT_CHANNELS — comma-separated channel IDs to also respond in
//	                     (DMs always work without this)

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SlackProvider is a ChatProvider implementation used for Slack-bot requests.
// It is currently a placeholder and may be extended to satisfy any required
// interfaces used by RunBotAgent.
type SlackProvider struct{}

// RunSlackBot starts the Slack Socket Mode bot and blocks until a fatal error.
func RunSlackBot(cfg *config.Config) error {
	if cfg.SlackToken == "" {
		return fmt.Errorf("SLACK_TOKEN is not set")
	}
	if cfg.SlackAppToken == "" {
		return fmt.Errorf("SLACK_APP_TOKEN is not set (xapp-... token required for Socket Mode)")
	}
	if cfg.AnthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	provider := SlackProvider{}

	allowedChannels := map[string]bool{}
	for _, ch := range strings.Split(cfg.SlackBotChannels, ",") {
		ch = strings.TrimSpace(ch)
		if ch != "" {
			allowedChannels[ch] = true
		}
	}

	api := slack.New(
		cfg.SlackToken,
		slack.OptionAppLevelToken(cfg.SlackAppToken),
	)

	client := socketmode.New(api)

	go func() {
		for evt := range client.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				if evt.Request != nil {
					client.Ack(*evt.Request)
				}
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				go handleSlackMessage(cfg, api, provider, allowedChannels, eventsAPIEvent)
			}
		}
	}()

	log.Printf("Caboose-MCP Slack bot is online (Socket Mode, channels: %q, DMs: always)", cfg.SlackBotChannels)
	return client.Run()
}

func handleSlackMessage(cfg *config.Config, api *slack.Client, provider SlackProvider, allowedChannels map[string]bool, event slackevents.EventsAPIEvent) {
	ev, ok := event.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return
	}

	// Ignore bots and message edits/deletes
	if ev.BotID != "" || ev.SubType != "" {
		return
	}

	isDM := ev.ChannelType == "im"
	inAllowedChannel := allowedChannels[ev.Channel]
	if !isDM && !inAllowedChannel {
		return
	}

	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return
	}

	reply, err := RunBotAgent(context.Background(), cfg, provider, text)
	if err != nil {
		log.Printf("slack bot agent error for channel %s: %v", ev.Channel, err)
		// Send a generic error message to the user and log any failure to post it.
		_, ts, _, postErr := api.PostMessage(ev.Channel, slack.MsgOptionText("Sorry, something went wrong while processing your request.", false))
		if postErr != nil {
			log.Printf("failed to post Slack error message to channel %s: %v", ev.Channel, postErr)
		} else {
			log.Printf("posted Slack error message to channel %s at %s", ev.Channel, ts)
		}
		return
	}

	for _, chunk := range splitMessage(reply, 3000) {
		channelID, ts, _, postErr := api.PostMessage(ev.Channel, slack.MsgOptionText(chunk, false))
		if postErr != nil {
			log.Printf("failed to post Slack message to channel %s: %v", ev.Channel, postErr)
			continue
		}
		log.Printf("posted Slack message to channel %s at %s", channelID, ts)
	}
}

// RunBotAgent is a minimal bot-agent loop used by the Slack gateway.
// It currently echoes the input text so that the Slack bot remains functional
// even without a more advanced shared agent implementation.
func RunBotAgent(ctx context.Context, cfg *config.Config, provider SlackProvider, input string) (string, error) {
	// TODO: wire this up to the shared Claude agent loop when available.
	return input, nil
}

// splitMessage splits a long message into chunks of at most maxLen characters.
// It prefers to split on newline or space boundaries to keep messages readable.
func splitMessage(s string, maxLen int) []string {
	if maxLen <= 0 {
		return []string{}
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}

	if len(s) <= maxLen {
		return []string{s}
	}

	var parts []string
	remaining := s

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			parts = append(parts, remaining)
			break
		}

		cut := maxLen

		// Try to split at a newline within the limit.
		if idx := strings.LastIndex(remaining[:cut], "\n"); idx != -1 {
			cut = idx + 1
		} else if idx := strings.LastIndex(remaining[:cut], " "); idx != -1 {
			// Fallback to last space within the limit.
			cut = idx + 1
		}

		chunk := strings.TrimRight(remaining[:cut], "\n ")
		if chunk != "" {
			parts = append(parts, chunk)
		}

		remaining = strings.TrimLeft(remaining[cut:], "\n ")
	}

	return parts
}

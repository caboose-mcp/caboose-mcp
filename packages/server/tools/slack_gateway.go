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
	"os"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

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

	client := socketmode.New(api,
		socketmode.OptionDebug(true),
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	go func() {
		for evt := range client.Events {
			log.Printf("[slack debug] event type: %s", evt.Type)
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				if evt.Request != nil {
					client.Ack(*evt.Request)
				}
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					log.Printf("[slack debug] could not cast to EventsAPIEvent")
					continue
				}
				log.Printf("[slack debug] events api event type: %s", eventsAPIEvent.Type)
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

	userKey := "slack:" + ev.User
	reply, err := RunBotAgent(context.Background(), cfg, provider, userKey, text)
	if err != nil {
		log.Printf("slack bot agent error for channel %s: %v", ev.Channel, err)
		// Send a generic error message to the user and log any failure to post it.
		_, ts, postErr := api.PostMessage(ev.Channel, slack.MsgOptionText("Sorry, something went wrong while processing your request.", false))
		if postErr != nil {
			log.Printf("failed to post Slack error message to channel %s: %v", ev.Channel, postErr)
		} else {
			log.Printf("posted Slack error message to channel %s at %s", ev.Channel, ts)
		}
		return
	}

	for _, chunk := range splitMessage(reply, 3000) {
		channelID, ts, postErr := api.PostMessage(ev.Channel, slack.MsgOptionText(chunk, false))
		if postErr != nil {
			log.Printf("failed to post Slack message to channel %s: %v", ev.Channel, postErr)
			continue
		}
		log.Printf("posted Slack message to channel %s at %s", channelID, ts)
	}
}

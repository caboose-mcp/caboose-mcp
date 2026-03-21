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
	"bytes"
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

// SlackSender implements PlatformSender for Slack.
type SlackSender struct {
	api *slack.Client
}

func (s SlackSender) SendText(channelID, text string) (string, error) {
	_, ts, err := s.api.PostMessage(channelID, slack.MsgOptionText(text, false))
	if err != nil {
		return "", err
	}
	return ts, nil
}

func (s SlackSender) SendAudio(channelID string, audio []byte) error {
	_, err := s.api.UploadFileV2(slack.UploadFileV2Parameters{
		Channel:  channelID,
		Filename: "caboose.mp3",
		FileSize: len(audio),
		Reader:   bytes.NewReader(audio),
		Title:    "Voice response",
	})
	return err
}

func (s SlackSender) SendTyping(channelID string) {
	// Slack Socket Mode has no typing indicator API — no-op
}

func (s SlackSender) MaxMessageLen() int {
	return 2800 // Slack's limit is 4000, we reserve 1200 for safety
}

func (s SlackSender) StartThread(channelID, messageID, name string) (string, error) {
	// Slack threads are anchored by message timestamp (thread_ts), not a separate channel.
	// The PlatformSender abstraction treats threadID as a channel for SendText, which is
	// invalid for Slack. Return the message timestamp here so SendTextInThread can use it
	// as thread_ts when posting replies.
	return messageID, nil
}

// SendTextInThread posts a message into a Slack thread.
// In Slack, threads require posting to the same channelID with a thread_ts option.
func (s SlackSender) SendTextInThread(channelID, threadTS, text string) (string, error) {
	_, ts, err := s.api.PostMessage(channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return "", err
	}
	return ts, nil
}

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
				go handleSlackMessage(cfg, api, SlackSender{api: api}, SlackProvider{sender: SlackSender{api: api}}, allowedChannels, eventsAPIEvent)
			}
		}
	}()

	log.Printf("Caboose-MCP Slack bot is online (Socket Mode, channels: %q, DMs: always)", cfg.SlackBotChannels)
	return client.Run()
}

func handleSlackMessage(cfg *config.Config, api *slack.Client, sender SlackSender, provider SlackProvider, allowedChannels map[string]bool, event slackevents.EventsAPIEvent) {
	var msg IncomingMessage
	var ok bool
	var channelForBusy string

	switch ev := event.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		msg, ok = parseSlackMessage(ev, allowedChannels)
		channelForBusy = ev.Channel
	case *slackevents.AppMentionEvent:
		msg, ok = parseSlackAppMention(ev)
		channelForBusy = ev.Channel
	default:
		return
	}

	if !ok {
		return
	}

	if !EnqueueBotMessage(context.Background(), cfg, msg, sender, provider) {
		api.PostMessage(channelForBusy, slack.MsgOptionText("⚔️ I'm mid-battle — try again in a moment.", false))
	}
}

// parseSlackAppMention extracts and validates an IncomingMessage from a Slack app_mention event.
// App mention events fire when someone @-mentions the bot in a channel.
func parseSlackAppMention(ev *slackevents.AppMentionEvent) (IncomingMessage, bool) {
	if ev.BotID != "" {
		return IncomingMessage{}, false
	}

	// Strip the @mention prefix (e.g. "<@U012AB3CD> ")
	text := strings.TrimSpace(ev.Text)
	// Remove any <@USERID> mention tokens from the start of the message
	for strings.HasPrefix(text, "<@") {
		end := strings.Index(text, ">")
		if end < 0 {
			break
		}
		text = strings.TrimSpace(text[end+1:])
	}
	if text == "" {
		return IncomingMessage{}, false
	}

	return IncomingMessage{
		UserKey:           "slack:" + ev.User,
		ChannelID:         ev.Channel,
		OriginalMessageID: ev.TimeStamp,
		Content:           text,
		IsDM:              false, // app_mention only fires in channels
	}, true
}

// parseSlackMessage extracts and validates an IncomingMessage from a Slack event.
// Returns (msg, false) if the message should be ignored (bot, empty, etc).
func parseSlackMessage(ev *slackevents.MessageEvent, allowedChannels map[string]bool) (IncomingMessage, bool) {
	// Ignore bots and message edits/deletes
	if ev.BotID != "" || ev.SubType != "" {
		return IncomingMessage{}, false
	}

	isDM := ev.ChannelType == "im"
	inAllowedChannel := allowedChannels[ev.Channel]
	if !isDM && !inAllowedChannel {
		return IncomingMessage{}, false
	}

	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return IncomingMessage{}, false
	}

	return IncomingMessage{
		UserKey:           "slack:" + ev.User,
		ChannelID:         ev.Channel,
		OriginalMessageID: ev.TimeStamp,
		Content:           text,
		IsDM:              isDM,
	}, true
}

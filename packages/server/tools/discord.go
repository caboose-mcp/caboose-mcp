package tools

// discord — Discord Bot API integration (v10) + Incoming Webhook support.
//
// Bot tools (require DISCORD_TOKEN):
//   discord_post_message  — post a message to a channel by ID
//   discord_list_channels — list channels in a guild (server) by ID
//   discord_read_messages — read recent messages from a channel
//
// Webhook tools (require DISCORD_WEBHOOK_URL):
//   discord_webhook_post  — post a message via Incoming Webhook (no bot token needed)
//                           Also used internally by EmitEvent for notifications.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const discordAPIBase = "https://discord.com/api/v10"

// DiscordWebhookPost sends a message to the configured DISCORD_WEBHOOK_URL.
// Called by EmitEvent for push notifications and by the discord_webhook_post tool.
func DiscordWebhookPost(webhookURL, content string) error {
	if webhookURL == "" {
		return fmt.Errorf("Discord webhook URL not configured")
	}
	body, _ := json.Marshal(map[string]string{"content": content})
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord webhook returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func RegisterDiscord(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("discord_post_message",
		mcp.WithDescription("Post a message to a Discord channel."),
		mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Message content")),
	), discordPostMessageHandler(cfg))

	s.AddTool(mcp.NewTool("discord_list_channels",
		mcp.WithDescription("List channels in a Discord guild."),
		mcp.WithString("guild_id", mcp.Required(), mcp.Description("Discord guild (server) ID")),
	), discordListChannelsHandler(cfg))

	s.AddTool(mcp.NewTool("discord_webhook_post",
		mcp.WithDescription("Post a message to Discord via Incoming Webhook. "+
			"No bot token required — uses DISCORD_WEBHOOK_URL. "+
			"Ideal for notifications, alerts, and tool results."),
		mcp.WithString("content", mcp.Required(), mcp.Description("Message content (max 2000 chars)")),
		mcp.WithString("webhook_url", mcp.Description("Override the default DISCORD_WEBHOOK_URL for this call")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content := req.GetString("content", "")
		url := req.GetString("webhook_url", cfg.DiscordWebhookURL)
		if err := DiscordWebhookPost(url, content); err != nil {
			return mcp.NewToolResultText("error: " + err.Error()), nil
		}
		return mcp.NewToolResultText("Message posted via webhook."), nil
	})

	s.AddTool(mcp.NewTool("discord_read_messages",
		mcp.WithDescription("Read recent messages from a Discord channel."),
		mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID")),
		mcp.WithNumber("limit", mcp.Description("Max messages (default 20)")),
	), discordReadMessagesHandler(cfg))
}

func discordDo(cfg *config.Config, method, path string, body any) ([]byte, error) {
	if cfg.DiscordToken == "" {
		return nil, fmt.Errorf("Discord token not configured")
	}
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, discordAPIBase+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+cfg.DiscordToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("discord API error %d: %s", resp.StatusCode, data)
	}
	return data, nil
}

func discordPostMessageHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		channelID, err := req.RequireString("channel_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := discordDo(cfg, "POST", "/channels/"+channelID+"/messages", map[string]string{"content": content})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var msg map[string]any
		json.Unmarshal(data, &msg)
		id, _ := msg["id"].(string)
		return mcp.NewToolResultText(fmt.Sprintf("message posted, id=%s", id)), nil
	}
}

func discordListChannelsHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		guildID, err := req.RequireString("guild_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := discordDo(cfg, "GET", "/guilds/"+guildID+"/channels", nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var channels []any
		json.Unmarshal(data, &channels)
		out, _ := json.MarshalIndent(channels, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

func discordReadMessagesHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		channelID, err := req.RequireString("channel_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 20)
		data, err := discordDo(cfg, "GET", "/channels/"+channelID+"/messages?limit="+strconv.Itoa(limit), nil)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var messages []any
		json.Unmarshal(data, &messages)
		out, _ := json.MarshalIndent(messages, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

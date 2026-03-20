package tools

// slack — Slack Web API integration.
//
// Requires SLACK_TOKEN set to a Bot OAuth token (xoxb-...) with the following
// scopes: chat:write, channels:read, channels:history.
//
// Tools:
//   slack_post_message  — post a message to a channel (supports thread replies)
//   slack_list_channels — list channels the bot has access to
//   slack_read_messages — read recent messages from a channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterSlack(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("slack_post_message",
		mcp.WithDescription("Post a message to a Slack channel."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel ID or name")),
		mcp.WithString("text", mcp.Required(), mcp.Description("Message text")),
		mcp.WithString("thread_ts", mcp.Description("Thread timestamp to reply to")),
	), slackPostMessageHandler(cfg))

	s.AddTool(mcp.NewTool("slack_list_channels",
		mcp.WithDescription("List Slack channels the bot has access to."),
		mcp.WithNumber("limit", mcp.Description("Max results (default 100)")),
	), slackListChannelsHandler(cfg))

	s.AddTool(mcp.NewTool("slack_read_messages",
		mcp.WithDescription("Read recent messages from a Slack channel."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel ID")),
		mcp.WithNumber("limit", mcp.Description("Max messages (default 20)")),
	), slackReadMessagesHandler(cfg))
}

func slackAPICall(cfg *config.Config, method, endpoint string, body any) (map[string]any, error) {
	if cfg.SlackToken == "" {
		return nil, fmt.Errorf("`slack_post_message` is not yet set up.\n\nTo configure it, set SLACK_TOKEN=<xoxb-your-token> in your environment or .env file.")
	}
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "https://slack.com/api/"+endpoint, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.SlackToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if ok, _ := result["ok"].(bool); !ok {
		errMsg, _ := result["error"].(string)
		return nil, fmt.Errorf("slack API error: %s", errMsg)
	}
	return result, nil
}

func slackPostMessageHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		channel, err := req.RequireString("channel")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := req.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body := map[string]any{"channel": channel, "text": text}
		if ts := req.GetString("thread_ts", ""); ts != "" {
			body["thread_ts"] = ts
		}
		result, err := slackAPICall(cfg, "POST", "chat.postMessage", body)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ts, _ := result["ts"].(string)
		return mcp.NewToolResultText(fmt.Sprintf("message posted, ts=%s", ts)), nil
	}
}

func slackListChannelsHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if cfg.SlackToken == "" {
			return mcp.NewToolResultError("`slack_list_channels` is not yet set up.\n\nTo configure it, set SLACK_TOKEN=<xoxb-your-token> in your environment or .env file."), nil
		}
		limit := req.GetInt("limit", 100)
		u := fmt.Sprintf("https://slack.com/api/conversations.list?limit=%d&types=public_channel,private_channel", limit)
		httpReq, _ := http.NewRequest("GET", u, nil)
		httpReq.Header.Set("Authorization", "Bearer "+cfg.SlackToken)
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer resp.Body.Close()
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if ok, _ := result["ok"].(bool); !ok {
			e, _ := result["error"].(string)
			return mcp.NewToolResultError("slack error: " + e), nil
		}
		channels, _ := result["channels"].([]any)
		out, _ := json.MarshalIndent(channels, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

func slackReadMessagesHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if cfg.SlackToken == "" {
			return mcp.NewToolResultError("`slack_read_messages` is not yet set up.\n\nTo configure it, set SLACK_TOKEN=<xoxb-your-token> in your environment or .env file."), nil
		}
		channel, err := req.RequireString("channel")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 20)
		params := url.Values{}
		params.Set("channel", channel)
		params.Set("limit", strconv.Itoa(limit))
		u := "https://slack.com/api/conversations.history?" + params.Encode()
		httpReq, _ := http.NewRequest("GET", u, nil)
		httpReq.Header.Set("Authorization", "Bearer "+cfg.SlackToken)
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer resp.Body.Close()
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if ok, _ := result["ok"].(bool); !ok {
			e, _ := result["error"].(string)
			return mcp.NewToolResultError("slack error: " + e), nil
		}
		messages, _ := result["messages"].([]any)
		out, _ := json.MarshalIndent(messages, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

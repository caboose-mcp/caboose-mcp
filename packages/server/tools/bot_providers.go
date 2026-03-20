package tools

// bot_providers.go — Discord and Slack OAuth2 providers for bot integration.
// Allows users to authorize per-workspace or per-user bot tokens via scoped JWTs.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/caboose-mcp/server/config"
)

// Discord OAuth2 constants for bot provider
const (
	discordBotOAuthURL  = "https://discord.com/api/oauth2/authorize"
	discordBotTokenURL  = "https://discord.com/api/oauth2/token"
	discordBotAPIScopes = "bot applications.commands"
	discordBotClientID  = "DISCORD_CLIENT_ID"
	discordBotClientSec = "DISCORD_CLIENT_SECRET"
	discordRedirectURI  = "DISCORD_REDIRECT_URI"
)

// Slack OAuth2 constants for bot provider
const (
	slackBotOAuthURL  = "https://slack.com/oauth/v2/authorize"
	slackBotTokenURL  = "https://slack.com/api/oauth.v2.access"
	slackBotAPIScopes = "chat:write channels:read channels:history commands"
	slackBotClientID  = "SLACK_CLIENT_ID"
	slackBotClientSec = "SLACK_CLIENT_SECRET"
)

// DiscordBotProvider implements OAuthProvider for Discord bots.
type DiscordBotProvider struct{}

var discordBotProvider = &DiscordBotProvider{}

// SlackBotProvider implements OAuthProvider for Slack bots.
type SlackBotProvider struct{}

var slackBotProvider = &SlackBotProvider{}

// ---- Discord Bot Provider ----

func (p *DiscordBotProvider) Name() string {
	return "discord_bot"
}

func (p *DiscordBotProvider) RequiredJWTScopes() []string {
	return []string{"discord_bot"}
}

func (p *DiscordBotProvider) TokenPath(claudeDir, jti string) string {
	if jti != "" {
		return filepath.Join(claudeDir, "discord", "bot-token-"+jti+".json")
	}
	return filepath.Join(claudeDir, "discord", "bot-token.json")
}

func (p *DiscordBotProvider) HasToken(ctx context.Context, cfg *config.Config) bool {
	claims := GetAuthClaims(ctx)
	jti := ""
	if claims != nil {
		jti = claims.JTI
	}
	_, err := os.Stat(p.TokenPath(cfg.ClaudeDir, jti))
	return err == nil
}

func (p *DiscordBotProvider) GetAuthURL(cfg *config.Config, state string) (string, error) {
	clientID := os.Getenv(discordBotClientID)
	if clientID == "" {
		return "", fmt.Errorf("DISCORD_CLIENT_ID not set")
	}
	redirectURI := os.Getenv(discordRedirectURI)
	if redirectURI == "" {
		return "", fmt.Errorf("DISCORD_REDIRECT_URI not set")
	}
	params := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"scope":         {discordBotAPIScopes},
		"response_type": {"code"},
		"permissions":   {"2048"},
	}
	if state != "" {
		params.Set("state", state)
	}
	return discordBotOAuthURL + "?" + params.Encode(), nil
}

func (p *DiscordBotProvider) ExchangeCode(ctx context.Context, cfg *config.Config, code string) error {
	clientID := os.Getenv(discordBotClientID)
	clientSec := os.Getenv(discordBotClientSec)
	redirectURI := os.Getenv(discordRedirectURI)
	if clientID == "" || clientSec == "" {
		return fmt.Errorf("DISCORD_CLIENT_ID or DISCORD_CLIENT_SECRET not set")
	}
	if redirectURI == "" {
		return fmt.Errorf("DISCORD_REDIRECT_URI not set")
	}

	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSec},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, discordBotTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("building token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading token response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("invalid OAuth error response (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("invalid response from Discord")
	}

	if tokenResp.Error != "" {
		return fmt.Errorf("auth error: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	token := botToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		TokenType:    tokenResp.TokenType,
	}

	if err := saveBotToken(ctx, cfg, p, token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	return nil
}

func (p *DiscordBotProvider) GetClient(ctx context.Context, cfg *config.Config) (*http.Client, error) {
	tok, err := loadBotToken(ctx, cfg, p)
	if err != nil {
		return nil, fmt.Errorf("%s", p.AuthErrorMessage(cfg))
	}

	// Discord bots use the "Bot" scheme, not "Bearer".
	return &http.Client{Transport: &discordBotTransport{token: tok.AccessToken}}, nil
}

func (p *DiscordBotProvider) AuthErrorMessage(cfg *config.Config) string {
	authURL, err := p.GetAuthURL(cfg, "")
	if err != nil {
		return "Discord bot not authorized. Ensure DISCORD_CLIENT_ID, DISCORD_CLIENT_SECRET, and DISCORD_REDIRECT_URI are set, then retry your request."
	}
	return fmt.Sprintf(
		"Discord bot not authorized.\n\nVisit this URL to authorize the bot:\n\n%s\n\n"+
			"After completing authorization in your browser, retry your request.",
		authURL)
}

// ---- Slack Bot Provider ----

func (p *SlackBotProvider) Name() string {
	return "slack_bot"
}

func (p *SlackBotProvider) RequiredJWTScopes() []string {
	return []string{"slack_bot"}
}

func (p *SlackBotProvider) TokenPath(claudeDir, jti string) string {
	if jti != "" {
		return filepath.Join(claudeDir, "slack", "bot-token-"+jti+".json")
	}
	return filepath.Join(claudeDir, "slack", "bot-token.json")
}

func (p *SlackBotProvider) HasToken(ctx context.Context, cfg *config.Config) bool {
	claims := GetAuthClaims(ctx)
	jti := ""
	if claims != nil {
		jti = claims.JTI
	}
	_, err := os.Stat(p.TokenPath(cfg.ClaudeDir, jti))
	return err == nil
}

func (p *SlackBotProvider) GetAuthURL(cfg *config.Config, state string) (string, error) {
	clientID := os.Getenv(slackBotClientID)
	if clientID == "" {
		return "", fmt.Errorf("SLACK_CLIENT_ID not set")
	}
	params := url.Values{
		"client_id": {clientID},
		"scope":     {slackBotAPIScopes},
	}
	if state != "" {
		params.Set("state", state)
	}
	return slackBotOAuthURL + "?" + params.Encode(), nil
}

func (p *SlackBotProvider) ExchangeCode(ctx context.Context, cfg *config.Config, code string) error {
	clientID := os.Getenv(slackBotClientID)
	clientSec := os.Getenv(slackBotClientSec)
	if clientID == "" || clientSec == "" {
		return fmt.Errorf("SLACK_CLIENT_ID or SLACK_CLIENT_SECRET not set")
	}

	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSec},
		"code":          {code},
		"grant_type":    {"authorization_code"},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, slackBotTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("building token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading token response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("invalid OAuth error response (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		OK          bool   `json:"ok"`
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("invalid response from Slack")
	}

	if !tokenResp.OK {
		return fmt.Errorf("auth error: %s", tokenResp.Error)
	}

	// Slack doesn't provide refresh tokens; tokens are long-lived.
	token := botToken{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
	}

	if err := saveBotToken(ctx, cfg, p, token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	return nil
}

func (p *SlackBotProvider) GetClient(ctx context.Context, cfg *config.Config) (*http.Client, error) {
	tok, err := loadBotToken(ctx, cfg, p)
	if err != nil {
		return nil, fmt.Errorf("%s", p.AuthErrorMessage(cfg))
	}

	return &http.Client{Transport: &botBearerTransport{token: tok.AccessToken}}, nil
}

func (p *SlackBotProvider) AuthErrorMessage(cfg *config.Config) string {
	authURL, err := p.GetAuthURL(cfg, "")
	if err != nil {
		return "Slack bot not authorized. Ensure SLACK_CLIENT_ID and SLACK_CLIENT_SECRET are set, then retry your request."
	}
	return fmt.Sprintf(
		"Slack bot not authorized.\n\nVisit this URL to authorize the bot:\n\n%s\n\n"+
			"After completing authorization in your browser, retry your request.",
		authURL)
}

// ---- Shared bot token storage ----

type botToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	TokenType    string `json:"token_type"`
}

type botBearerTransport struct {
	token string
}

func (t *botBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// discordBotTransport is an http.RoundTripper that attaches Discord's Bot token scheme.
type discordBotTransport struct {
	token string
}

func (t *discordBotTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bot "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

func loadBotToken(ctx context.Context, cfg *config.Config, provider OAuthProvider) (botToken, error) {
	var tok botToken
	claims := GetAuthClaims(ctx)
	jti := ""
	if claims != nil {
		jti = claims.JTI
	}
	path := provider.TokenPath(cfg.ClaudeDir, jti)
	data, err := os.ReadFile(path)
	if err != nil {
		return tok, err
	}
	return tok, json.Unmarshal(data, &tok)
}

func saveBotToken(ctx context.Context, cfg *config.Config, provider OAuthProvider, tok botToken) error {
	claims := GetAuthClaims(ctx)
	jti := ""
	if claims != nil {
		jti = claims.JTI
	}
	path := provider.TokenPath(cfg.ClaudeDir, jti)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

package tools

// oauth_provider.go — Pluggable OAuth2 provider interface for external service integrations.
// Currently implements Google Calendar OAuth2. Designed to support GitHub, Slack, etc. in future.

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
	"time"

	"github.com/caboose-mcp/server/config"
)

// Helper functions for oauth_provider.go — kept private to avoid duplication with calendar.go

func readResponseBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// OAuthProvider is a pluggable OAuth2 provider backed by on-disk token storage.
// Each provider is identified by a stable name string (e.g., "google_calendar").
type OAuthProvider interface {
	// Name returns the stable provider identifier.
	Name() string

	// RequiredJWTScopes returns the scope strings that must appear in the
	// provider-specific scope claim field within JWTClaims for a request to
	// be allowed to use this provider. Google OAuth providers check
	// JWTClaims.GoogleScopes; Discord and Slack bot providers check their
	// respective JWTClaims.DiscordScopes / JWTClaims.SlackScopes fields.
	// Empty slice = no JWT scope restriction (admin / open-mode always passes).
	RequiredJWTScopes() []string

	// TokenPath returns the filesystem path where this provider's OAuth token
	// is stored for the given JTI. jti may be "" for the global (admin) token.
	TokenPath(claudeDir, jti string) string

	// HasToken reports whether a usable token file exists on disk for the
	// context's JTI. Does NOT validate expiry — that is GetClient's job.
	HasToken(ctx context.Context, cfg *config.Config) bool

	// GetAuthURL returns the consent URL the user should visit to authorize
	// this provider. state is an opaque string passed through for CSRF protection;
	// for OOB flow it can be "".
	GetAuthURL(cfg *config.Config, state string) (string, error)

	// ExchangeCode exchanges an authorization code for a token and saves it
	// to disk at TokenPath.
	ExchangeCode(ctx context.Context, cfg *config.Config, code string) error

	// GetClient returns an authenticated *http.Client. If the access token is
	// within the 30-second expiry window it auto-refreshes. On failure it
	// returns an error produced by AuthErrorMessage so callers can surface the
	// consent URL directly to the user.
	GetClient(ctx context.Context, cfg *config.Config) (*http.Client, error)

	// AuthErrorMessage returns a human-readable error string that includes the
	// full consent URL. Called by GetClient when no token file exists or
	// refresh has failed.
	AuthErrorMessage(cfg *config.Config) string
}

// GoogleCalendarProvider implements OAuthProvider for Google Calendar OAuth2.
// It is a zero-value-safe, stateless struct; safe for concurrent use.
type GoogleCalendarProvider struct{}

var googleCalendarProvider = &GoogleCalendarProvider{}

// ---- GoogleCalendarProvider methods ----

func (p *GoogleCalendarProvider) Name() string {
	return "google_calendar"
}

func (p *GoogleCalendarProvider) RequiredJWTScopes() []string {
	return []string{
		"https://www.googleapis.com/auth/calendar.readonly",
		"https://www.googleapis.com/auth/calendar",
	}
}

func (p *GoogleCalendarProvider) TokenPath(claudeDir, jti string) string {
	if jti != "" {
		return filepath.Join(claudeDir, "google", "calendar-token-"+jti+".json")
	}
	return filepath.Join(claudeDir, "google", "calendar-token.json")
}

func (p *GoogleCalendarProvider) HasToken(ctx context.Context, cfg *config.Config) bool {
	claims := GetAuthClaims(ctx)
	jti := ""
	if claims != nil {
		jti = claims.JTI
	}
	_, err := os.Stat(p.TokenPath(cfg.ClaudeDir, jti))
	return err == nil
}

func (p *GoogleCalendarProvider) GetAuthURL(cfg *config.Config, state string) (string, error) {
	creds, err := loadGoogleCredentials(cfg)
	if err != nil {
		return "", fmt.Errorf("credentials not found: %w", err)
	}
	params := url.Values{
		"client_id":     {creds.Installed.ClientID},
		"redirect_uri":  {"urn:ietf:wg:oauth:2.0:oob"},
		"response_type": {"code"},
		"scope":         {calendarScope},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}
	if state != "" {
		params.Set("state", state)
	}
	return googleAuthURL + "?" + params.Encode(), nil
}

func (p *GoogleCalendarProvider) ExchangeCode(ctx context.Context, cfg *config.Config, code string) error {
	creds, err := loadGoogleCredentials(cfg)
	if err != nil {
		return fmt.Errorf("credentials not found: %w", err)
	}
	data := url.Values{
		"code":          {code},
		"client_id":     {creds.Installed.ClientID},
		"client_secret": {creds.Installed.ClientSecret},
		"redirect_uri":  {"urn:ietf:wg:oauth:2.0:oob"},
		"grant_type":    {"authorization_code"},
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("building token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}

	// Read and parse response
	body, err := readResponseBody(resp)
	if err != nil {
		return fmt.Errorf("reading token response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("invalid OAuth error response (HTTP %d): %s", resp.StatusCode, string(body))
	}

	if err := jsonUnmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("invalid response from Google")
	}

	if tokenResp.Error != "" {
		return fmt.Errorf("auth error: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	token := googleToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		TokenType:    tokenResp.TokenType,
	}

	if err := saveGoogleToken(ctx, cfg, token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	return nil
}

func (p *GoogleCalendarProvider) GetClient(ctx context.Context, cfg *config.Config) (*http.Client, error) {
	tok, err := loadGoogleToken(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%s", p.AuthErrorMessage(cfg))
	}

	// Check if refresh needed (30-second window before expiry)
	if time.Now().After(tok.Expiry.Add(-30 * time.Second)) {
		// If no refresh token, we can't refresh — return auth error immediately
		if tok.RefreshToken == "" {
			return nil, fmt.Errorf("%s", p.AuthErrorMessage(cfg))
		}

		creds, err := loadGoogleCredentials(cfg)
		if err != nil {
			return nil, fmt.Errorf("%s", p.AuthErrorMessage(cfg))
		}

		tok, err = refreshGoogleToken(ctx, cfg, tok, creds)
		if err != nil {
			return nil, fmt.Errorf("%s", p.AuthErrorMessage(cfg))
		}
	}

	return &http.Client{Transport: &calBearerTransport{token: tok.AccessToken}}, nil
}

func (p *GoogleCalendarProvider) AuthErrorMessage(cfg *config.Config) string {
	authURL, err := p.GetAuthURL(cfg, "")
	if err != nil {
		return fmt.Sprintf(
			"Google Calendar not authorized. Set up credentials at %s, then call calendar_auth_url.",
			googleCredentialsPath(cfg))
	}
	return fmt.Sprintf(
		"Google Calendar not authorized.\n\nVisit this URL to authorize:\n\n%s\n\n"+
			"Then call: calendar_auth_complete(code=\"<paste code here>\")",
		authURL)
}

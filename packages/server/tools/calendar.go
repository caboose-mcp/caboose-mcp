package tools

// calendar — Google Calendar integration + local date utilities.
//
// Google Calendar OAuth2 setup:
//   1. console.cloud.google.com → APIs & Services → Credentials
//   2. Create OAuth2 Client ID (Desktop app type), download credentials.json
//   3. Save to CLAUDE_DIR/google/credentials.json
//      (or set GOOGLE_CALENDAR_CREDENTIALS env var to override path)
//   4. Call calendar_auth_url, visit the URL, paste the code into calendar_auth_complete.
//
// Tokens are stored at CLAUDE_DIR/google/calendar-token.json and auto-refreshed.

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
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	calendarScope  = "https://www.googleapis.com/auth/calendar"
	calendarAPIv3  = "https://www.googleapis.com/calendar/v3"
)

type googleCredentials struct {
	Installed struct {
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		RedirectURIs []string `json:"redirect_uris"`
	} `json:"installed"`
}

type googleToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	TokenType    string    `json:"token_type"`
}

type calBearerTransport struct{ token string }

func (t *calBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

func RegisterCalendar(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("calendar_today",
		mcp.WithDescription("Return today's date, day of week, week number, and days until common events."),
	), calendarTodayHandler(cfg))

	s.AddTool(mcp.NewTool("calendar_auth_url",
		mcp.WithDescription("Generate the Google Calendar OAuth2 consent URL. Visit it in a browser, then paste the code into calendar_auth_complete."),
	), calendarAuthURLHandler(cfg))

	s.AddTool(mcp.NewTool("calendar_auth_complete",
		mcp.WithDescription("Complete Google Calendar OAuth2 setup by exchanging the authorization code for a token."),
		mcp.WithString("code", mcp.Required(), mcp.Description("Authorization code from the Google consent URL")),
	), calendarAuthCompleteHandler(cfg))

	s.AddTool(mcp.NewTool("calendar_list",
		mcp.WithDescription("List upcoming Google Calendar events."),
		mcp.WithNumber("days", mcp.Description("How many days ahead to look (default 7, max 90)")),
		mcp.WithString("calendar_id", mcp.Description("Calendar ID to query (default: primary)")),
	), calendarListHandler(cfg))

	s.AddTool(mcp.NewTool("calendar_create",
		mcp.WithDescription("Create a Google Calendar event."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Event title")),
		mcp.WithString("start", mcp.Required(), mcp.Description("Start time: RFC3339 or 'YYYY-MM-DD HH:MM' (local timezone)")),
		mcp.WithString("end", mcp.Description("End time (same formats). Defaults to 1 hour after start.")),
		mcp.WithString("details", mcp.Description("Event description/notes")),
		mcp.WithString("calendar_id", mcp.Description("Calendar ID (default: primary)")),
	), calendarCreateHandler(cfg))

	s.AddTool(mcp.NewTool("calendar_delete",
		mcp.WithDescription("Delete a Google Calendar event by its ID (from calendar_list output)."),
		mcp.WithString("event_id", mcp.Required(), mcp.Description("Event ID from calendar_list")),
		mcp.WithString("calendar_id", mcp.Description("Calendar ID (default: primary)")),
	), calendarDeleteHandler(cfg))
}

func calendarTodayHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		now := time.Now()
		_, week := now.ISOWeek()
		result := fmt.Sprintf(
			"Date:    %s\nDay:     %s\nWeek:    %d\nTime:    %s\nUnix:    %d",
			now.Format("2006-01-02"),
			now.Weekday().String(),
			week,
			now.Format("15:04:05 MST"),
			now.Unix(),
		)
		return mcp.NewToolResultText(result), nil
	}
}

func calendarAuthURLHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		authURL, err := googleCalendarProvider.GetAuthURL(cfg, "")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(
				"credentials not found: %v\n\nSetup:\n1. console.cloud.google.com → APIs & Services → Credentials\n2. Create OAuth2 Client ID (Desktop app type)\n3. Download credentials.json → save to %s",
				err, googleCredentialsPath(cfg))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"Visit this URL to authorize Google Calendar:\n\n%s\n\nThen: calendar_auth_complete(code=\"<code>\")", authURL)), nil
	}
}

func calendarAuthCompleteHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		code, err := req.RequireString("code")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := googleCalendarProvider.ExchangeCode(ctx, cfg, code); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// Tell the user where the token was saved (per-user or global).
		tokenFile := googleTokenPath(ctx, cfg)
		return mcp.NewToolResultText(fmt.Sprintf(
			"Google Calendar authorized. Token saved to %s.\nYou can now use calendar_list, calendar_create, and calendar_delete.", tokenFile)), nil
	}
}

func calendarListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		days := req.GetInt("days", 7)
		if days < 1 {
			days = 7
		}
		if days > 90 {
			days = 90
		}
		calID := req.GetString("calendar_id", "primary")
		client, err := googleCalendarClient(ctx, cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		now := time.Now()
		apiURL := fmt.Sprintf("%s/calendars/%s/events?timeMin=%s&timeMax=%s&singleEvents=true&orderBy=startTime&maxResults=50",
			calendarAPIv3, url.PathEscape(calID),
			url.QueryEscape(now.Format(time.RFC3339)),
			url.QueryEscape(now.AddDate(0, 0, days).Format(time.RFC3339)))
		body, err := calAPIGet(client, apiURL)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var result struct {
			Items []struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
				Start   struct {
					DateTime string `json:"dateTime"`
					Date     string `json:"date"`
				} `json:"start"`
				End struct {
					DateTime string `json:"dateTime"`
					Date     string `json:"date"`
				} `json:"end"`
				Description string `json:"description"`
			} `json:"items"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return mcp.NewToolResultError("failed to parse calendar response"), nil
		}
		if len(result.Items) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No events in the next %d days.", days)), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Upcoming events (next %d days):\n\n", days))
		for _, ev := range result.Items {
			start := ev.Start.DateTime
			if start == "" {
				start = ev.Start.Date
			}
			end := ev.End.DateTime
			if end == "" {
				end = ev.End.Date
			}
			sb.WriteString(fmt.Sprintf("• %s\n  ID: %s\n  %s → %s\n", ev.Summary, ev.ID, start, end))
			if ev.Description != "" {
				sb.WriteString(fmt.Sprintf("  Notes: %s\n", strings.ReplaceAll(ev.Description, "\n", " ")))
			}
			sb.WriteString("\n")
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func calendarCreateHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := checkGoogleScope(ctx, "https://www.googleapis.com/auth/calendar"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		startStr, err := req.RequireString("start")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		start, err := parseEventTime(startStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid start time: %v", err)), nil
		}
		end := start.Add(time.Hour)
		if endStr := req.GetString("end", ""); endStr != "" {
			if end, err = parseEventTime(endStr); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid end time: %v", err)), nil
			}
		}
		calID := req.GetString("calendar_id", "primary")
		client, err := googleCalendarClient(ctx, cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		event := map[string]any{
			"summary":     title,
			"description": req.GetString("details", ""),
			"start":       map[string]string{"dateTime": start.Format(time.RFC3339)},
			"end":         map[string]string{"dateTime": end.Format(time.RFC3339)},
		}
		eventJSON, _ := json.Marshal(event)
		resp, err := client.Post(
			fmt.Sprintf("%s/calendars/%s/events", calendarAPIv3, url.PathEscape(calID)),
			"application/json", strings.NewReader(string(eventJSON)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create event: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var created struct {
			ID    string `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &created); err != nil {
			return mcp.NewToolResultError("invalid response from Calendar API"), nil
		}
		if created.Error != nil {
			return mcp.NewToolResultError(created.Error.Message), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Event created: %q (ID: %s)\n%s → %s",
			title, created.ID, start.Format("Mon Jan 2 15:04"), end.Format("Mon Jan 2 15:04"))), nil
	}
}

func calendarDeleteHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := checkGoogleScope(ctx, "https://www.googleapis.com/auth/calendar"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		eventID, err := req.RequireString("event_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		calID := req.GetString("calendar_id", "primary")
		client, err := googleCalendarClient(ctx, cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		httpReq, _ := http.NewRequest("DELETE",
			fmt.Sprintf("%s/calendars/%s/events/%s", calendarAPIv3, url.PathEscape(calID), url.PathEscape(eventID)), nil)
		resp, err := client.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete: %v", err)), nil
		}
		defer resp.Body.Close()
		if resp.StatusCode == 204 {
			return mcp.NewToolResultText(fmt.Sprintf("Event %s deleted.", eventID)), nil
		}
		body, _ := io.ReadAll(resp.Body)
		return mcp.NewToolResultError(fmt.Sprintf("delete failed (HTTP %d): %s", resp.StatusCode, body)), nil
	}
}

// ---- Google OAuth2 helpers ----

func googleCredentialsPath(cfg *config.Config) string {
	if v := os.Getenv("GOOGLE_CALENDAR_CREDENTIALS"); v != "" {
		return v
	}
	return filepath.Join(cfg.ClaudeDir, "google", "credentials.json")
}

// googleTokenPath returns the token file path. When a JWT identity is present
// in ctx it returns a per-JTI path so each user has isolated Google tokens.
func googleTokenPath(ctx context.Context, cfg *config.Config) string {
	claims := GetAuthClaims(ctx)
	jti := ""
	if claims != nil {
		jti = claims.JTI
	}
	return googleCalendarProvider.TokenPath(cfg.ClaudeDir, jti)
}

// checkGoogleScope returns an error if the JWT in ctx exists but does not
// include requiredScope. Admin/unauthenticated requests always pass.
func checkGoogleScope(ctx context.Context, requiredScope string) error {
	claims := GetAuthClaims(ctx)
	if claims == nil || len(claims.GoogleScopes) == 0 {
		return nil
	}
	for _, s := range claims.GoogleScopes {
		if s == requiredScope || s == "https://www.googleapis.com/auth/calendar" {
			return nil
		}
	}
	return fmt.Errorf("insufficient Google scope: token has %v, need %s",
		claims.GoogleScopes, requiredScope)
}

func loadGoogleCredentials(cfg *config.Config) (googleCredentials, error) {
	var creds googleCredentials
	data, err := os.ReadFile(googleCredentialsPath(cfg))
	if err != nil {
		return creds, err
	}
	return creds, json.Unmarshal(data, &creds)
}

func loadGoogleToken(ctx context.Context, cfg *config.Config) (googleToken, error) {
	var tok googleToken
	data, err := os.ReadFile(googleTokenPath(ctx, cfg))
	if err != nil {
		return tok, err
	}
	return tok, json.Unmarshal(data, &tok)
}

func saveGoogleToken(ctx context.Context, cfg *config.Config, tok googleToken) error {
	path := googleTokenPath(ctx, cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(tok, "", "  ")
	return os.WriteFile(path, b, 0600)
}

func refreshGoogleToken(ctx context.Context, cfg *config.Config, tok googleToken, creds googleCredentials) (googleToken, error) {
	data := url.Values{
		"client_id":     {creds.Installed.ClientID},
		"client_secret": {creds.Installed.ClientSecret},
		"refresh_token": {tok.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	resp, err := http.PostForm(googleTokenURL, data)
	if err != nil {
		return tok, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return tok, fmt.Errorf("invalid OAuth error response (HTTP %d): %s", resp.StatusCode, body)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return tok, fmt.Errorf("invalid refresh response")
	}
	if result.Error != "" {
		return tok, fmt.Errorf("token refresh error: %s", result.Error)
	}
	tok.AccessToken = result.AccessToken
	tok.Expiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	_ = saveGoogleToken(ctx, cfg, tok)
	return tok, nil
}

func googleCalendarClient(ctx context.Context, cfg *config.Config) (*http.Client, error) {
	return googleCalendarProvider.GetClient(ctx, cfg)
}

func calAPIGet(client *http.Client, apiURL string) ([]byte, error) {
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, body)
	}
	return body, nil
}

func parseEventTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized format; use RFC3339 or 'YYYY-MM-DD HH:MM'")
}

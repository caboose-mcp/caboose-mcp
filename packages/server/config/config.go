package config

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	ClaudeDir       string
	GPGKeyID        string
	SlackToken      string
	DiscordToken    string
	BambuIP         string
	BambuAccessCode string
	BambuSerial     string
	BambuBedTemp    int
	BambuNozzleTemp int
	GreptileAPIKey  string
	GreptileRepo    string
	PostgresURL     string
	MongoURL        string
	// Cloud sync
	GitHubToken string // GITHUB_TOKEN or resolved from `gh auth token`
	// n8n integration
	N8nWebhookURL string // N8N_WEBHOOK_URL
	N8nAPIKey     string // N8N_API_KEY (optional, for header auth)
	// Chat bot
	AnthropicAPIKey    string // ANTHROPIC_API_KEY
	DiscordWebhookURL  string // DISCORD_WEBHOOK_URL — incoming webhook for outbound notifications
	DiscordBotChannels string // DISCORD_BOT_CHANNELS — comma-separated channel IDs
	SlackAppToken      string // SLACK_APP_TOKEN — xapp-... for Socket Mode
	SlackBotChannels   string // SLACK_BOT_CHANNELS — comma-separated channel IDs
	// ElevenLabs TTS
	ElevenLabsAPIKey  string // ELEVENLABS_API_KEY
	ElevenLabsVoiceID string // ELEVENLABS_VOICE_ID — required to enable TTS
	// Release stage flag — controls experimental banner and MCP disclaimer.
	// "experimental" (default) → warnings shown everywhere.
	// "stable" → all warnings suppressed.
	ReleaseStage string // CABOOSE_ENV
	// UIOrigin is the allowed CORS origin for the standalone UI.
	UIOrigin string // MCP_UI_ORIGIN (default: https://ui.mcp.chrismarasco.io)
}

func Load() *Config {
	homeDir, _ := os.UserHomeDir()

	claudeDir := os.Getenv("CLAUDE_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(homeDir, ".claude")
	}

	bedTemp := 55
	if v := os.Getenv("BAMBU_BED_TEMP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			bedTemp = n
		}
	}

	nozzleTemp := 220
	if v := os.Getenv("BAMBU_NOZZLE_TEMP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			nozzleTemp = n
		}
	}

	greptileRepo := os.Getenv("GREPTILE_REPO")
	if greptileRepo == "" {
		greptileRepo = "github/caboose/caboose-mcp"
	}

	// GitHub token: env var first, then fall back to `gh auth token`
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
			githubToken = strings.TrimSpace(string(out))
		}
	}

	return &Config{
		ClaudeDir:       claudeDir,
		GitHubToken:     githubToken,
		GPGKeyID:        os.Getenv("GPG_KEY_ID"),
		SlackToken:      os.Getenv("SLACK_TOKEN"),
		DiscordToken:    os.Getenv("DISCORD_TOKEN"),
		BambuIP:         os.Getenv("BAMBU_IP"),
		BambuAccessCode: os.Getenv("BAMBU_ACCESS_CODE"),
		BambuSerial:     os.Getenv("BAMBU_SERIAL"),
		BambuBedTemp:    bedTemp,
		BambuNozzleTemp: nozzleTemp,
		GreptileAPIKey:  os.Getenv("GREPTILE_API_KEY"),
		GreptileRepo:    greptileRepo,
		PostgresURL:     os.Getenv("POSTGRES_URL"),
		MongoURL:        os.Getenv("MONGO_URL"),
		N8nWebhookURL:      os.Getenv("N8N_WEBHOOK_URL"),
		N8nAPIKey:          os.Getenv("N8N_API_KEY"),
		AnthropicAPIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		DiscordWebhookURL:  os.Getenv("DISCORD_WEBHOOK_URL"),
		DiscordBotChannels: os.Getenv("DISCORD_BOT_CHANNELS"),
		SlackAppToken:      os.Getenv("SLACK_APP_TOKEN"),
		SlackBotChannels:   os.Getenv("SLACK_BOT_CHANNELS"),
		ElevenLabsAPIKey:   os.Getenv("ELEVENLABS_API_KEY"),
		ElevenLabsVoiceID:  os.Getenv("ELEVENLABS_VOICE_ID"),
		ReleaseStage:       releaseStage(),
		UIOrigin:           uiOrigin(),
	}
}

// uiOrigin returns the allowed CORS origin for the standalone UI.
func uiOrigin() string {
	const defaultOrigin = "https://ui.mcp.chrismarasco.io"

	v := strings.TrimSpace(os.Getenv("MCP_UI_ORIGIN"))
	if v == "" {
		return defaultOrigin
	}

	// Normalize by removing any trailing slashes to avoid malformed URLs like "//".
	v = strings.TrimRight(v, "/")

	// Validate that the value is a well-formed http(s) URL.
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return defaultOrigin
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return defaultOrigin
	}

	return v
}

// releaseStage reads CABOOSE_ENV and normalises to "experimental" or "stable".
func releaseStage() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CABOOSE_ENV")))
	if v == "stable" {
		return "stable"
	}
	return "experimental"
}

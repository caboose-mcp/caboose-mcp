package tui

// RunSetup is an interactive terminal wizard that walks through every
// configurable env var, shows the current value, and lets the user update it.
// At the end it writes a .env file to the path of their choice.
//
// Invoked with: ./caboose-mcp --setup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caboose-mcp/server/config"
)

type wizardField struct {
	key         string
	label       string
	description string
	current     string
	secret      bool // mask value in display
	optional    bool
}

func RunSetup(cfg *config.Config) error {
	scanner := bufio.NewScanner(os.Stdin)

	printHeader()

	fields := buildFields(cfg)

	fmt.Println("For each setting, press Enter to keep the current value or type a new one.")
	fmt.Println("Type '-' to clear a value. Secrets are masked.")
	fmt.Println()

	// Group fields by section
	sections := []struct {
		name   string
		fields []string
	}{
		{"Core", []string{"CLAUDE_DIR", "GPG_KEY_ID"}},
		{"Messaging", []string{"SLACK_TOKEN", "DISCORD_TOKEN"}},
		{"n8n Integration", []string{"N8N_WEBHOOK_URL", "N8N_API_KEY"}},
		{"GitHub", []string{"GITHUB_TOKEN"}},
		{"Databases", []string{"POSTGRES_URL", "MONGO_URL"}},
		{"Bambu 3D Printer", []string{"BAMBU_IP", "BAMBU_ACCESS_CODE", "BAMBU_SERIAL", "BAMBU_BED_TEMP", "BAMBU_NOZZLE_TEMP"}},
		{"Greptile", []string{"GREPTILE_API_KEY", "GREPTILE_REPO"}},
		{"Cloud Sync", []string{"CLOUDSYNC_S3_BUCKET"}},
	}

	fieldMap := make(map[string]*wizardField)
	for i := range fields {
		fieldMap[fields[i].key] = &fields[i]
	}

	for _, section := range sections {
		fmt.Printf("\n── %s %s\n", section.name, strings.Repeat("─", max(0, 40-len(section.name)-4)))
		for _, key := range section.fields {
			f, ok := fieldMap[key]
			if !ok {
				continue
			}
			promptField(scanner, f)
		}
	}

	// Summary + write
	fmt.Println()
	fmt.Println("─────────────────────────────────────────")
	fmt.Println("Review your settings:")
	fmt.Println()
	for _, f := range fields {
		display := f.current
		if display == "" {
			display = "(not set)"
		} else if f.secret {
			display = mask(f.current)
		}
		fmt.Printf("  %-24s %s\n", f.key+"=", display)
	}

	fmt.Println()
	envPath := prompt(scanner, "Write .env to (Enter for ./"+".env):", ".env")
	if !filepath.IsAbs(envPath) {
		cwd, _ := os.Getwd()
		envPath = filepath.Join(cwd, envPath)
	}

	if err := writeEnvFile(envPath, fields); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}

	fmt.Printf("\nWrote %s\n", envPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Load the env file:   export $(grep -v '^#' .env | xargs)")
	fmt.Println("  2. Or add it to your shell profile / service manager")
	fmt.Println("  3. Re-run setup_check from Claude to verify:")
	fmt.Println("     ./caboose-mcp --tui   (or ask Claude: setup_check)")
	fmt.Println()

	return nil
}

func buildFields(cfg *config.Config) []wizardField {
	homeDir, _ := os.UserHomeDir()
	defaultClaudeDir := filepath.Join(homeDir, ".claude")

	return []wizardField{
		{key: "CLAUDE_DIR", label: "Claude data directory", description: "Where caboose-mcp stores sessions, secrets, sources, etc.", current: getEnvOr("CLAUDE_DIR", defaultClaudeDir), optional: true},
		{key: "GPG_KEY_ID", label: "GPG key ID", description: "Used to encrypt secrets. Find with: gpg --list-keys", current: cfg.GPGKeyID, optional: true},
		{key: "SLACK_TOKEN", label: "Slack bot token", description: "xoxb-... Bot OAuth token from api.slack.com/apps", current: cfg.SlackToken, secret: true, optional: true},
		{key: "DISCORD_TOKEN", label: "Discord bot token", description: "Bot token from discord.com/developers/applications", current: cfg.DiscordToken, secret: true, optional: true},
		{key: "N8N_WEBHOOK_URL", label: "n8n webhook URL", description: "Webhook node URL for push events (e.g. http://localhost:5678/webhook/caboose-events)", current: cfg.N8nWebhookURL, optional: true},
		{key: "N8N_API_KEY", label: "n8n API key", description: "Optional: sent as X-N8N-API-KEY header on webhook calls", current: cfg.N8nAPIKey, secret: true, optional: true},
		{key: "GITHUB_TOKEN", label: "GitHub token", description: "For cloud sync Gist backend (auto-resolved from `gh auth token` if unset)", current: cfg.GitHubToken, secret: true, optional: true},
		{key: "POSTGRES_URL", label: "PostgreSQL URL", description: "postgres://user:pass@host:5432/dbname", current: cfg.PostgresURL, secret: true, optional: true},
		{key: "MONGO_URL", label: "MongoDB URL", description: "mongodb://host:27017", current: cfg.MongoURL, optional: true},
		{key: "BAMBU_IP", label: "Bambu printer IP", description: "Local IP of your Bambu A1 printer", current: cfg.BambuIP, optional: true},
		{key: "BAMBU_ACCESS_CODE", label: "Bambu access code", description: "8-char code shown on the printer touchscreen", current: cfg.BambuAccessCode, optional: true},
		{key: "BAMBU_SERIAL", label: "Bambu serial number", description: "Printer serial number", current: cfg.BambuSerial, optional: true},
		{key: "BAMBU_BED_TEMP", label: "Bambu bed temp (°C)", description: "Default bed temperature (default: 55)", current: getEnvOr("BAMBU_BED_TEMP", "55"), optional: true},
		{key: "BAMBU_NOZZLE_TEMP", label: "Bambu nozzle temp (°C)", description: "Default nozzle temperature (default: 220)", current: getEnvOr("BAMBU_NOZZLE_TEMP", "220"), optional: true},
		{key: "GREPTILE_API_KEY", label: "Greptile API key", description: "From app.greptile.com", current: cfg.GreptileAPIKey, secret: true, optional: true},
		{key: "GREPTILE_REPO", label: "Greptile repo", description: "Default repo to query (e.g. github/owner/repo)", current: getEnvOr("GREPTILE_REPO", "github/caboose/caboose-mcp"), optional: true},
		{key: "CLOUDSYNC_S3_BUCKET", label: "Cloud sync S3 bucket", description: "S3 bucket name for config sync (optional, Gist uses GITHUB_TOKEN)", current: getEnvOr("CLOUDSYNC_S3_BUCKET", ""), optional: true},
	}
}

func promptField(scanner *bufio.Scanner, f *wizardField) {
	display := f.current
	if display == "" {
		display = "(not set)"
	} else if f.secret {
		display = mask(f.current)
	}

	fmt.Printf("  %s\n", f.label)
	if f.description != "" {
		fmt.Printf("  %s\n", styleGray(f.description))
	}
	fmt.Printf("  Current: %s\n", display)
	fmt.Printf("  New value: ")

	val := scanLine(scanner)
	switch val {
	case "":
		// keep current
	case "-":
		f.current = ""
	default:
		f.current = val
	}
	fmt.Println()
}

func writeEnvFile(path string, fields []wizardField) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("# caboose-mcp configuration\n")
	sb.WriteString("# Generated by: caboose-mcp --setup\n\n")

	for _, f := range fields {
		sb.WriteString(fmt.Sprintf("# %s\n", f.label))
		sb.WriteString(fmt.Sprintf("%s=%s\n\n", f.key, f.current))
	}
	return os.WriteFile(path, []byte(sb.String()), 0600)
}

// ---- helpers ----

func printHeader() {
	fmt.Println()
	fmt.Println("  ╔═══════════════════════════════════════╗")
	fmt.Println("  ║       caboose-mcp setup wizard        ║")
	fmt.Println("  ╚═══════════════════════════════════════╝")
	fmt.Println()
}

func prompt(scanner *bufio.Scanner, label, defaultVal string) string {
	fmt.Printf("%s ", label)
	val := scanLine(scanner)
	if val == "" {
		return defaultVal
	}
	return val
}

func scanLine(scanner *bufio.Scanner) string {
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func mask(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

func styleGray(s string) string {
	return "\033[90m" + s + "\033[0m"
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

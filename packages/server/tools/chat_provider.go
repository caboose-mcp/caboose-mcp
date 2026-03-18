package tools

// ChatProvider is the abstraction layer for chat platform integrations.
// Implement this interface to add new providers (Discord, Slack, Telegram, etc.)
// without changing the shared bot agent logic.
//
// Each provider is responsible for:
//   - Identifying itself by name (used in system prompt formatting hints)
//   - Adapting standard markdown to whatever the platform renders
type ChatProvider interface {
	// Name returns the provider identifier, e.g. "discord" or "slack".
	Name() string
	// FormatText adapts a response string for the platform's markdown renderer.
	FormatText(text string) string
}

// DiscordProvider implements ChatProvider for Discord.
// Discord renders **bold**, *italic*, `code`, ```blocks```, > quotes, and emoji.
// It does NOT render # headers — we strip those.
type DiscordProvider struct{}

func (DiscordProvider) Name() string { return "discord" }
func (DiscordProvider) FormatText(text string) string {
	// Discord renders most standard markdown natively.
	// Strip leading # headers — replace with **bold** equivalent.
	var out []string
	for _, line := range splitLines(text) {
		if len(line) > 2 && line[0] == '#' {
			i := 0
			for i < len(line) && (line[i] == '#' || line[i] == ' ') {
				i++
			}
			out = append(out, "**"+line[i:]+"**")
		} else {
			out = append(out, line)
		}
	}
	return joinLines(out)
}

// SlackProvider implements ChatProvider for Slack.
// Slack uses mrkdwn: *bold*, _italic_, `code`, > quotes.
// Standard **bold** and *italic* do not render — we convert them.
type SlackProvider struct{}

func (SlackProvider) Name() string { return "slack" }
func (SlackProvider) FormatText(text string) string {
	// Convert Discord/standard markdown → Slack mrkdwn
	// **bold** → *bold*   *italic* → _italic_   # Header → *Header*
	result := text
	result = replaceMarkdown(result, "**", "*")
	result = replaceMarkdown(result, "*", "_")
	var out []string
	for _, line := range splitLines(result) {
		if len(line) > 2 && line[0] == '#' {
			i := 0
			for i < len(line) && (line[i] == '#' || line[i] == ' ') {
				i++
			}
			out = append(out, "*"+line[i:]+"*")
		} else {
			out = append(out, line)
		}
	}
	return joinLines(out)
}

// splitLines / joinLines are small helpers used by providers.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}

// replaceMarkdown swaps a markdown delimiter for another (e.g. "**" → "*").
// Handles paired delimiters only (open + close).
func replaceMarkdown(s, from, to string) string {
	result := ""
	open := false
	i := 0
	for i < len(s) {
		if i+len(from) <= len(s) && s[i:i+len(from)] == from {
			if open {
				result += to
			} else {
				result += to
			}
			open = !open
			i += len(from)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}

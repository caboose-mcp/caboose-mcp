package tools

// MessageCacher is an optional interface that PlatformSender implementations may satisfy
// to cache the full bot reply keyed by message ID (e.g. for on-demand TTS via reactions).
type MessageCacher interface {
	CacheReply(msgID, fullReply string)
}

// IncomingMessage is a platform-normalized inbound message.
// Used by dispatchMessage to orchestrate bot responses across platforms.
type IncomingMessage struct {
	UserKey           string // "<platform>:<userID>" — used as bot_memory key
	ChannelID         string // reply destination (channel ID, DM ID, thread ID)
	OriginalMessageID string // used by StartThread to anchor the reply thread
	Content           string // cleaned text with mentions stripped
	IsDM              bool   // true if this is a direct message
}

// PlatformSender abstracts reply delivery for any chat platform.
// Implemented by DiscordSender and SlackSender; used by dispatchMessage.
type PlatformSender interface {
	// SendText posts a text message to a channel and returns the message ID.
	SendText(channelID, text string) (messageID string, err error)
	// SendTextInThread posts a text message into an existing thread.
	// For Discord, threadID is the thread channel ID (threads are channels).
	// For Slack, channelID is the parent channel and threadID is the thread_ts timestamp.
	SendTextInThread(channelID, threadID, text string) (messageID string, err error)
	// SendAudio posts an audio file (MP3) to a channel.
	SendAudio(channelID string, audio []byte) error
	// SendTyping shows a typing indicator; platforms may implement as no-op if unsupported.
	SendTyping(channelID string)
	// MaxMessageLen returns the maximum characters per message for this platform.
	MaxMessageLen() int
	// StartThread creates a thread anchored to a message (optional; return "" if unsupported).
	StartThread(channelID, messageID, name string) (threadID string, err error)
}

// ChatProvider is the abstraction layer for chat platform integrations.
// Implement this interface to add new providers (Discord, Slack, Telegram, etc.)
// without changing the shared bot agent logic.
//
// Each provider is responsible for:
//   - Identifying itself by name (used in system prompt formatting hints)
//   - Adapting standard markdown to whatever the platform renders
//   - Providing a PlatformSender for reply delivery (used by dispatchMessage)
type ChatProvider interface {
	// Name returns the provider identifier, e.g. "discord" or "slack".
	Name() string
	// FormatText adapts a response string for the platform's markdown renderer.
	FormatText(text string) string
	// Sender returns the PlatformSender for this provider (used by dispatchMessage).
	Sender() PlatformSender
}

// DiscordProvider implements ChatProvider for Discord.
// Discord renders **bold**, *italic*, `code`, ```blocks```, > quotes, and emoji.
// It does NOT render # headers — we strip those.
type DiscordProvider struct {
	sender PlatformSender
}

func (d DiscordProvider) Name() string           { return "discord" }
func (d DiscordProvider) Sender() PlatformSender { return d.sender }
func (d DiscordProvider) FormatText(text string) string {
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
type SlackProvider struct {
	sender PlatformSender
}

func (s SlackProvider) Name() string           { return "slack" }
func (s SlackProvider) Sender() PlatformSender { return s.sender }
func (s SlackProvider) FormatText(text string) string {
	// Convert Discord/standard markdown → Slack mrkdwn
	// **bold** → *bold*   *italic* → _italic_   # Header → *Header*
	result := slackConvertMarkdown(text)
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

// slackConvertMarkdown does a single-pass conversion of common markdown to Slack mrkdwn.
// It converts:
//   - **bold** → *bold*  (Slack bold)
//   - *italic* → _italic_ (Slack italic)
//
// It preserves list bullets like "* item" at the start of a line.
func slackConvertMarkdown(text string) string {
	result := ""
	i := 0
	for i < len(text) {
		// Bold: **...** → *...*
		if i+2 <= len(text) && text[i:i+2] == "**" {
			result += "*"
			i += 2
			continue
		}

		// Italic or bullet: *...* → _..._ , but keep "* " at start of line as a bullet.
		if text[i] == '*' {
			isLineStart := i == 0 || text[i-1] == '\n'
			nextIsSpace := i+1 < len(text) && text[i+1] == ' '
			if isLineStart && nextIsSpace {
				// Preserve list bullet "* "
				result += "*"
				i++
				continue
			}
			// Treat as italic delimiter
			result += "_"
			i++
			continue
		}

		// Default: copy character
		result += string(text[i])
		i++
	}
	return result
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
// It performs a straightforward substring replacement; callers ensure pairing if needed.
func replaceMarkdown(s, from, to string) string {
	result := ""
	i := 0
	for i < len(s) {
		if i+len(from) <= len(s) && s[i:i+len(from)] == from {
			result += to
			i += len(from)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}

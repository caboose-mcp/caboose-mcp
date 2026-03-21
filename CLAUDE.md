# caboose-mcp Development Guidelines

Specialized rules for working on the caboose-mcp codebase.

## Architecture Overview

**Module**: `github.com/caboose-mcp/server`
**Language**: Go 1.24.2
**Primary structure**:
- `main.go` — Entry point, server builder, tool registration
- `packages/server/tools/` — All MCP tool implementations
- `packages/server/tui/` — Bubbletea terminal UI
- `packages/server/config/` — Environment-driven configuration

## Tool Registration Tiers

Three registration functions organize tools by deployment context:
- `registerCommonTools()` — Available everywhere (jokes, Claude API)
- `registerHostedTools()` — Cloud-safe, no local hardware (dev tools: si_*, github_*, db, etc.)
- `registerLocalTools()` — Local hardware only (Docker, printer, chezmoi, etc.)

**Key**: When adding/removing tools, always verify which tier they belong in.

## Bot Agent Tiers

Two distinct bot agent tool sets:
- `buildMobileTools()` (deprecated) — Lifestyle tools (calendar, learning, focus, notes)
- `buildDevTools()` — Dev-focused tools (si_*, github_*, health_report, joke)

**Current default**: `buildDevTools` is active. Only switch if specific use case requires lifestyle tools.

## Session Optimization & Token Efficiency

### TodoWrite Batching (Yo-Yo Queue)
When using TodoWrite to track progress:
- **Buffer updates** in memory instead of firing after every change
- **Batch and flush** when: **20+ items queued** OR **5+ minutes elapsed** (whichever comes first)
- **Goal**: Reduce API calls while keeping visibility high
- Exception: Flush immediately if user explicitly asks for status

### File Reading Strategy
When editing multiple sections of the same file:
- **Read the entire file once** at the start (use `Read` without limit)
- **Plan all edits** based on full context
- **Execute edits sequentially** in order they appear in file
- **Rationale**: Avoids offset drift and multiple file reads

### Session Improvement Skills
- **Skip `/improve-session` skill** if encoding issues are known or session has non-ASCII content
- Prefer manual analysis over broken tooling
- Document blockers in a note for later investigation

## Code Style & Patterns

- **Error handling**: Wrap errors with context, avoid silent failures
- **Config**: All environment-based, use `config.Load()` pattern
- **Tool handlers**: Signature is always `func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)`
- **TUI updates**: Use Bubbletea messages (tickMsg, statusMsg, detailMsg, etc.) for model updates
- **File watching**: Use fsnotify for real-time state updates (not polling)

## Testing & Validation

- Build frequently: `go build -o caboose-mcp .`
- Check lints: `go fmt ./...` and `go vet ./...`
- Run TUI: `./caboose-mcp --tui` to verify UI changes
- Run Discord bot: `./caboose-mcp --bots` to test bot agent

## Branch Naming & PRs

- Feature branches: `feat/description`
- Fix branches: `fix/description`
- Commit messages: Clear, present-tense, with co-author line
- PRs: Link to plan file in description, test plan required

# vscode-extension

A VS Code extension that connects to a running [caboose-mcp](https://github.com/caboose-mcp/server) binary over stdio and exposes its tools directly inside the editor.

## Features

- **Sidebar panel** — browse all MCP tools grouped by category (Audit, Calendar, Chezmoi, Focus, etc.)
- **Run tools** — click any tool to execute it; required and optional parameters are collected via VS Code input prompts
- **Status bar** — optional polling widget that calls a configurable MCP tool on an interval (defaults to `focus_status`)
- **Filter tools** — restrict which tools are loaded using an allowlist
- **Auto-connect** — connects automatically on startup when `binaryPath` is set

## Prerequisites

- [caboose-mcp](https://github.com/caboose-mcp/server) binary built and available on your system
- VS Code 1.85+

## Installation

1. Clone this repository and open it in VS Code.
2. Run `npm install` to install dev dependencies.
3. Press `F5` to launch the extension in a new Extension Development Host window.

To package the extension:

```bash
npx vsce package
```

Then install the generated `.vsix` file via **Extensions: Install from VSIX…**.

## Configuration

All settings live under the `cabooseMcp` namespace.

| Setting | Default | Description |
|---|---|---|
| `cabooseMcp.binaryPath` | `""` | Absolute path to the `caboose-mcp` binary |
| `cabooseMcp.env` | `{}` | Extra environment variables passed to the binary (e.g. `SLACK_TOKEN`, `DISCORD_TOKEN`) |
| `cabooseMcp.enabledTools` | `["*"]` | Tool allowlist. Use `["*"]` to load all tools, or list specific names like `["focus_start", "focus_end"]` |
| `cabooseMcp.autoConnect` | `true` | Connect automatically when VS Code starts |
| `cabooseMcp.statusBar.enabled` | `true` | Show the status bar item |
| `cabooseMcp.statusBar.tool` | `"focus_status"` | MCP tool to poll for status bar text |
| `cabooseMcp.statusBar.intervalSeconds` | `30` | Polling interval in seconds |

### Example `settings.json`

```json
{
  "cabooseMcp.binaryPath": "/home/you/go/bin/caboose-mcp",
  "cabooseMcp.env": {
    "SLACK_TOKEN": "xoxb-...",
    "DISCORD_TOKEN": "...",
    "GPG_KEY_ID": "ABCD1234"
  },
  "cabooseMcp.enabledTools": ["*"],
  "cabooseMcp.statusBar.tool": "focus_status",
  "cabooseMcp.statusBar.intervalSeconds": 30
}
```

## Usage

1. Set `cabooseMcp.binaryPath` in your VS Code settings.
2. Open the **Caboose MCP** panel in the Activity Bar (robot icon).
3. The extension connects automatically (or click **Connect** in the panel title bar).
4. Expand a tool group and click a tool to run it.
5. The output channel **Caboose MCP** shows all results.

## Commands

| Command | Description |
|---|---|
| `cabooseMcp.connect` | Connect to the MCP binary |
| `cabooseMcp.disconnect` | Disconnect from the MCP binary |
| `cabooseMcp.refresh` | Reload the tool list |
| `cabooseMcp.openSettings` | Open extension settings |

## Development

```bash
npm install        # install dependencies
npm run compile    # compile TypeScript once
npm run watch      # compile on file changes
```

Source layout:

```
src/
  extension.ts     # activation entry point, command handlers
  mcpClient.ts     # stdio JSON-RPC 2.0 MCP client
  toolsProvider.ts # TreeDataProvider for the sidebar
  statusBar.ts     # status bar polling widget
  config.ts        # typed settings helpers
```

## License

MIT

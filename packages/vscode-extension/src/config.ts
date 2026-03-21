import * as vscode from 'vscode';

/** Configuration for the status bar polling widget. */
export interface StatusBarConfig {
    /** Whether the status bar item is visible. */
    enabled: boolean;
    /** Name of the MCP tool to call on each poll cycle. */
    tool: string;
    /** How often (in seconds) to poll the tool. */
    intervalSeconds: number;
}

/** Resolved extension configuration read from VS Code workspace settings. */
export interface CabooseConfig {
    /** Absolute path to the fafb binary (stdio transport only). */
    binaryPath: string;
    /** Extra environment variables forwarded to the binary process. */
    env: Record<string, string>;
    /** Transport to use: `http` (default) connects to a running server; `stdio` spawns the binary. */
    transport: 'http' | 'stdio';
    /** Hostname of the fafb HTTP server (http transport only). */
    host: string;
    /** Port of the fafb HTTP server (http transport only). */
    port: number;
    /**
     * Tool allowlist. Use `["*"]` to load all tools, or list specific tool
     * names to restrict which ones appear in the sidebar.
     */
    enabledTools: string[];
    /** Connect automatically when the extension activates. */
    autoConnect: boolean;
    /** Status bar widget settings. */
    statusBar: StatusBarConfig;
}

/**
 * Reads the `cabooseMcp` workspace configuration and returns it as a typed
 * {@link CabooseConfig} object with defaults applied.
 */
export function getConfig(): CabooseConfig {
    const cfg = vscode.workspace.getConfiguration('cabooseMcp');
    return {
        binaryPath: cfg.get<string>('binaryPath', ''),
        env: cfg.get<Record<string, string>>('env', {}),
        transport: cfg.get<'http' | 'stdio'>('transport', 'http'),
        host: cfg.get<string>('host', 'localhost'),
        port: cfg.get<number>('port', 3000),
        enabledTools: cfg.get<string[]>('enabledTools', ['*']),
        autoConnect: cfg.get<boolean>('autoConnect', true),
        statusBar: {
            enabled: cfg.get<boolean>('statusBar.enabled', true),
            tool: cfg.get<string>('statusBar.tool', 'focus_status'),
            intervalSeconds: cfg.get<number>('statusBar.intervalSeconds', 30),
        },
    };
}

/**
 * Returns `true` if `toolName` is permitted by the `enabledTools` allowlist.
 *
 * A list containing `"*"` allows every tool. Otherwise the tool name must
 * appear verbatim in the list.
 */
export function isToolEnabled(toolName: string, enabledTools: string[]): boolean {
    if (enabledTools.includes('*')) return true;
    return enabledTools.includes(toolName);
}

import * as vscode from 'vscode';
import { McpClient } from './mcpClient';
import { StatusBarConfig } from './config';

/**
 * Manages the Caboose MCP status bar item.
 *
 * When started, the item polls a configurable MCP tool at a fixed interval and
 * updates its label with the first line of the tool's output. Has special
 * handling for `focus_status` to display the active focus goal as a short
 * label with a `$(target)` icon.
 *
 * The item shows `$(debug-disconnect)` when disconnected and `$(plug)` when
 * connected but no tool result is available yet.
 */
export class StatusBarManager implements vscode.Disposable {
    private item: vscode.StatusBarItem;
    private timer: NodeJS.Timeout | null = null;

    constructor(private readonly client: McpClient) {
        this.item = vscode.window.createStatusBarItem(
            vscode.StatusBarAlignment.Left,
            10,
        );
        this.item.command = 'cabooseMcp.connect';
        this.setDisconnected();
    }

    /**
     * Shows the status bar item and starts the polling interval.
     *
     * Immediately fires one poll before the first interval elapses. Does
     * nothing if `cfg.enabled` is `false`.
     *
     * @param cfg Status bar configuration from workspace settings.
     */
    start(cfg: StatusBarConfig): void {
        if (!cfg.enabled) return;

        this.item.show();
        this.setConnected();

        if (cfg.tool) {
            this.poll(cfg.tool);
            this.timer = setInterval(
                () => this.poll(cfg.tool),
                cfg.intervalSeconds * 1_000,
            );
        }
    }

    /**
     * Clears the polling interval and resets the item to the disconnected
     * appearance. Does not hide the item.
     */
    stop(): void {
        if (this.timer) {
            clearInterval(this.timer);
            this.timer = null;
        }
        this.setDisconnected();
    }

    /** Stops polling and disposes the underlying status bar item. */
    dispose(): void {
        this.stop();
        this.item.dispose();
    }

    /**
     * Calls `toolName` with no arguments and updates the status bar with the
     * result. Falls back to the generic connected appearance on error.
     */
    private async poll(toolName: string): Promise<void> {
        if (!this.client.connected) return;
        try {
            const result = await this.client.callTool(toolName, {});
            this.setToolResult(toolName, result);
        } catch {
            this.setConnected();
        }
    }

    /** Sets the item to the disconnected (click-to-connect) state. */
    private setDisconnected(): void {
        this.item.text = '$(debug-disconnect) caboose-mcp';
        this.item.tooltip = 'Caboose MCP: disconnected — click to connect';
        this.item.backgroundColor = undefined;
        this.item.command = 'cabooseMcp.connect';
    }

    /** Sets the item to the generic connected state (no tool result yet). */
    private setConnected(): void {
        this.item.text = '$(plug) caboose-mcp';
        this.item.tooltip = 'Caboose MCP: connected';
        this.item.backgroundColor = undefined;
        this.item.command = 'cabooseMcp.disconnect';
    }

    /**
     * Updates the status bar label from a tool result string.
     *
     * For `focus_status`, parses the "Focus: <goal>" line and truncates the
     * goal to 30 characters. For all other tools, shows the first line of
     * output truncated to 35 characters.
     *
     * @param toolName The tool that produced the result.
     * @param result   Raw text output from the tool.
     */
    private setToolResult(toolName: string, result: string): void {
        const firstLine = result.split('\n')[0].trim();

        if (toolName === 'focus_status') {
            if (result.includes('No active focus session')) {
                this.item.text = '$(target) no focus';
                this.item.tooltip = 'Caboose MCP: no active focus session';
            } else {
                const goal = firstLine.replace(/^Focus:\s*/i, '');
                const short = goal.length > 30 ? goal.slice(0, 27) + '...' : goal;
                this.item.text = `$(target) ${short}`;
                this.item.tooltip = `Focus: ${goal}\n\n${result}`;
            }
        } else {
            const short = firstLine.length > 35 ? firstLine.slice(0, 32) + '...' : firstLine;
            this.item.text = `$(plug) ${short}`;
            this.item.tooltip = `${toolName}:\n${result}`;
        }

        this.item.command = 'cabooseMcp.disconnect';
    }
}

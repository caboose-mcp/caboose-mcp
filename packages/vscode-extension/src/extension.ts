import * as vscode from 'vscode';
import { McpClient, Tool } from './mcpClient';
import { ToolsProvider } from './toolsProvider';
import { StatusBarManager } from './statusBar';
import { getConfig } from './config';

/**
 * VS Code extension entry point.
 *
 * Wires together the {@link McpClient}, {@link ToolsProvider}, and
 * {@link StatusBarManager}, registers all commands, and auto-connects if
 * `cabooseMcp.binaryPath` is configured.
 *
 * @param context Extension context provided by VS Code on activation.
 */
export async function activate(context: vscode.ExtensionContext): Promise<void> {
    const client = new McpClient();
    const output = vscode.window.createOutputChannel('Caboose MCP');
    const toolsProvider = new ToolsProvider(client);
    const statusBar = new StatusBarManager(client);

    output.show(true);
    output.appendLine('[fafb] extension activated');

    const treeView = vscode.window.createTreeView('cabooseMcp.tools', {
        treeDataProvider: toolsProvider,
        showCollapseAll: true,
    });

    client.on('info', (msg: string) => output.appendLine(`[fafb] ${msg}`));
    client.on('stderr', (data: string) => output.append(data));
    client.on('disconnected', () => {
        vscode.commands.executeCommand('setContext', 'cabooseMcp.connected', false);
        statusBar.stop();
        toolsProvider.clear();
        output.appendLine('[fafb] disconnected');
    });
    client.on('error', (err: Error) => {
        output.appendLine(`[fafb] error: ${err.message}`);
    });

    async function connect(): Promise<void> {
        const cfg = getConfig();

        if (cfg.transport === 'stdio') {
            if (!cfg.binaryPath) {
                const action = await vscode.window.showErrorMessage(
                    'cabooseMcp.binaryPath is not set (required for stdio transport).',
                    'Open Settings',
                );
                if (action) {
                    vscode.commands.executeCommand('workbench.action.openSettings', 'cabooseMcp.binaryPath');
                }
                return;
            }
            output.appendLine(`[fafb] connecting via stdio: ${cfg.binaryPath}`);
            try {
                await client.connect(cfg.binaryPath, cfg.env);
            } catch (err: unknown) {
                const msg = err instanceof Error ? err.message : String(err);
                output.appendLine(`[fafb] stdio connect failed: ${msg}`);
                vscode.window.showErrorMessage(`Caboose MCP: failed to connect — ${msg}`);
                return;
            }
        } else {
            const baseUrl = `http://${cfg.host}:${cfg.port}`;
            output.appendLine(`[fafb] connecting via HTTP: ${baseUrl}`);
            try {
                await client.connectHttp(baseUrl);
            } catch (err: unknown) {
                const e = err as NodeJS.ErrnoException;
                const msg = e?.message || e?.code || String(err);
                output.appendLine(`[fafb] HTTP connect failed: ${msg}`);
                if (e?.code) { output.appendLine(`[fafb]   code: ${e.code}`); }
                if (e?.stack) { output.appendLine(`[fafb]   ${e.stack}`); }
                output.appendLine(`[fafb] is the fafb server running at ${baseUrl}?`);
                vscode.window.showErrorMessage(`Caboose MCP: failed to connect to ${baseUrl} — ${msg}`);
                return;
            }
        }

        vscode.commands.executeCommand('setContext', 'cabooseMcp.connected', true);
        output.appendLine('[fafb] connected');
        await toolsProvider.loadTools(cfg.enabledTools);
        statusBar.start(cfg.statusBar);
    }

    function disconnect(): void {
        client.disconnect();
        vscode.commands.executeCommand('setContext', 'cabooseMcp.connected', false);
        output.appendLine('[fafb] disconnected by user');
    }

    /**
     * Prompts the user for each parameter defined in the tool's `inputSchema`,
     * then invokes the tool and prints the result to the output channel.
     *
     * - Required parameters block execution if dismissed.
     * - Optional parameters offer a Skip / Provide value choice.
     * - `enum` parameters use a QuickPick; `boolean` uses true/false QuickPick;
     *   all others use a free-text InputBox.
     *
     * @param tool The tool to execute, as selected from the sidebar.
     */
    async function runTool(tool: Tool): Promise<void> {
        const args: Record<string, unknown> = {};
        const schema = tool.inputSchema;
        const required: string[] = schema.required ?? [];
        const props = schema.properties ?? {};

        for (const [name, prop] of Object.entries(props)) {
            const isRequired = required.includes(name);

            if (!isRequired) {
                const choice = await vscode.window.showQuickPick(
                    ['Skip', 'Provide value'],
                    { placeHolder: `Optional: ${name} — ${prop.description ?? ''}`, title: tool.name },
                );
                if (choice !== 'Provide value') continue;
            }

            let value: unknown;

            if (prop.enum && prop.enum.length > 0) {
                value = await vscode.window.showQuickPick(prop.enum, {
                    placeHolder: prop.description ?? name,
                    title: `${tool.name} › ${name}`,
                });
            } else if (prop.type === 'boolean') {
                const pick = await vscode.window.showQuickPick(['true', 'false'], {
                    placeHolder: prop.description ?? name,
                    title: `${tool.name} › ${name}`,
                });
                value = pick === 'true' ? true : pick === 'false' ? false : undefined;
            } else {
                const raw = await vscode.window.showInputBox({
                    prompt: `${name}${isRequired ? ' (required)' : ' (optional)'}`,
                    placeHolder: prop.description ?? name,
                    title: `${tool.name} › ${name}`,
                    ignoreFocusOut: true,
                });
                if (raw === undefined && isRequired) {
                    vscode.window.showWarningMessage(
                        `Cancelled: "${name}" is required.`,
                    );
                    return;
                }
                value = prop.type === 'number' && raw !== undefined ? Number(raw) : raw;
            }

            if (value === undefined && isRequired) {
                vscode.window.showWarningMessage(`Cancelled: "${name}" is required.`);
                return;
            }
            if (value !== undefined) {
                args[name] = value;
            }
        }

        output.show(true);
        output.appendLine(
            `\n${'─'.repeat(60)}\n[${new Date().toLocaleTimeString()}] ${tool.name}`,
        );
        if (Object.keys(args).length) {
            output.appendLine(`args: ${JSON.stringify(args)}`);
        }

        try {
            const result = await client.callTool(tool.name, args);
            output.appendLine(result);
        } catch (err: unknown) {
            const msg = err instanceof Error ? err.message : String(err);
            output.appendLine(`ERROR: ${msg}`);
            vscode.window.showErrorMessage(`${tool.name} failed: ${msg}`);
        }
    }

    context.subscriptions.push(
        treeView,
        statusBar,
        output,
        client,
        vscode.commands.registerCommand('cabooseMcp.connect', connect),
        vscode.commands.registerCommand('cabooseMcp.disconnect', disconnect),
        vscode.commands.registerCommand('cabooseMcp.refresh', async () => {
            if (!client.connected) {
                await connect();
                return;
            }
            const cfg = getConfig();
            await toolsProvider.loadTools(cfg.enabledTools);
        }),
        vscode.commands.registerCommand('cabooseMcp.runTool', runTool),
        vscode.commands.registerCommand('cabooseMcp.openSettings', () => {
            vscode.commands.executeCommand(
                'workbench.action.openSettings',
                '@ext:caboose.vscode-fafb',
            );
        }),
        // Re-load tools when settings change (e.g. enabledTools filter)
        vscode.workspace.onDidChangeConfiguration(async (e) => {
            if (e.affectsConfiguration('cabooseMcp') && client.connected) {
                const cfg = getConfig();
                await toolsProvider.loadTools(cfg.enabledTools);
                statusBar.stop();
                statusBar.start(cfg.statusBar);
            }
        }),
    );

    // Auto-connect on startup
    const cfg = getConfig();
    if (cfg.autoConnect) {
        connect();
    }
}

/** Called by VS Code when the extension is deactivated; cleanup is handled via disposables. */
export function deactivate(): void {}

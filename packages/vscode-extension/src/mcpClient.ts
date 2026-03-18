import * as cp from 'child_process';
import * as readline from 'readline';
import * as http from 'http';
import * as https from 'https';
import { EventEmitter } from 'events';
import * as vscode from 'vscode';

/** A single parameter descriptor from an MCP tool's JSON Schema input definition. */
export interface ToolParam {
    /** JSON Schema primitive type (e.g. `"string"`, `"number"`, `"boolean"`). */
    type: string;
    /** Human-readable description shown in tool runner prompts. */
    description?: string;
    /** Fixed set of allowed string values; triggers a QuickPick instead of a free-text input. */
    enum?: string[];
}

/** An MCP tool as returned by the `tools/list` method. */
export interface Tool {
    /** Unique tool identifier (e.g. `focus_start`). */
    name: string;
    /** Short description of what the tool does. */
    description: string;
    /** JSON Schema describing the tool's accepted arguments. */
    inputSchema: {
        type: string;
        properties?: Record<string, ToolParam>;
        required?: string[];
    };
}

/** Internal state for an in-flight JSON-RPC request. */
interface PendingRequest {
    resolve: (value: unknown) => void;
    reject: (reason: Error) => void;
    /** Timeout handle; cleared when a response arrives. */
    timer: NodeJS.Timeout;
}

/**
 * Stdio-based MCP client.
 *
 * Spawns the caboose-mcp binary as a child process and communicates with it
 * using newline-delimited JSON-RPC 2.0 messages over stdin/stdout.
 *
 * **Lifecycle events** (via `EventEmitter`):
 * - `connected` — emitted after the MCP handshake completes
 * - `disconnected` — emitted when the child process exits
 * - `error` — emitted on process spawn/runtime errors
 * - `stderr` — emitted with raw stderr text from the child process
 */
export class McpClient extends EventEmitter implements vscode.Disposable {
    private proc: cp.ChildProcess | null = null;
    private sseReq: http.ClientRequest | null = null;
    private messagesUrl: string | null = null;
    private sessionId: string | null = null;
    private pending = new Map<number, PendingRequest>();
    private nextId = 1;

    /** `true` while connected via stdio, SSE, or streamable HTTP transport. */
    get connected(): boolean {
        return (this.proc !== null && !this.proc.killed) || this.sseReq !== null || this.messagesUrl !== null;
    }

    /**
     * Spawns the MCP binary and communicates over stdio.
     *
     * @param binaryPath Absolute path to the caboose-mcp executable.
     * @param env Additional environment variables merged into the child process environment.
     */
    async connect(binaryPath: string, env: Record<string, string>): Promise<void> {
        if (this.connected) this.disconnect();

        this.proc = cp.spawn(binaryPath, [], {
            stdio: ['pipe', 'pipe', 'pipe'],
            env: { ...process.env, ...env },
        });

        this.proc.stderr?.on('data', (data: Buffer) => {
            this.emit('stderr', data.toString());
        });

        this.proc.on('error', (err) => {
            this.emit('error', err);
            this.cleanupStdio();
        });

        this.proc.on('exit', (code) => {
            this.emit('disconnected', code);
            this.cleanupStdio();
        });

        const rl = readline.createInterface({ input: this.proc.stdout! });
        rl.on('line', (line) => this.handleLine(line));

        await this.initialize();
        this.emit('connected');
    }

    /**
     * Connects to a running caboose-mcp HTTP server.
     *
     * Tries the SSE transport (`/sse`) first. If the server returns 404,
     * falls back to streamable HTTP (`/mcp`) where each JSON-RPC request
     * is a POST and the response is returned inline.
     *
     * @param baseUrl Base URL of the server, e.g. `http://localhost:3000`.
     */
    async connectHttp(baseUrl: string): Promise<void> {
        if (this.connected) this.disconnect();
        try {
            await this.openSse(baseUrl);
        } catch (err) {
            const msg = (err as Error).message ?? '';
            if (msg.includes('404')) {
                // Server uses streamable HTTP — POST directly to /mcp
                this.emit('info', `SSE not available, trying streamable HTTP at ${baseUrl}/mcp`);
                this.messagesUrl = `${baseUrl}/mcp`;
            } else {
                throw err;
            }
        }
        await this.initialize();
        this.emit('connected');
    }

    /** Opens the SSE stream and resolves once the messages endpoint is known. */
    private openSse(baseUrl: string): Promise<void> {
        return new Promise((resolve, reject) => {
            const sseUrl = new URL('/sse', baseUrl);
            const mod = sseUrl.protocol === 'https:' ? https : http;
            let resolved = false;

            const req = mod.get(
                sseUrl.toString(),
                { headers: { Accept: 'text/event-stream' } },
                (res) => {
                    if (res.statusCode !== 200) {
                        reject(new Error(`SSE connect failed: HTTP ${res.statusCode}`));
                        res.resume();
                        return;
                    }

                    let buf = '';

                    res.on('data', (chunk: Buffer) => {
                        buf += chunk.toString();
                        const parts = buf.split('\n\n');
                        buf = parts.pop() ?? '';

                        for (const part of parts) {
                            let eventType = 'message';
                            let data = '';
                            for (const line of part.split('\n')) {
                                if (line.startsWith('event: ')) { eventType = line.slice(7).trim(); }
                                else if (line.startsWith('data: ')) { data = line.slice(6).trim(); }
                            }

                            if (eventType === 'endpoint' && data) {
                                this.messagesUrl = new URL(data, baseUrl).toString();
                                if (!resolved) { resolved = true; resolve(); }
                            } else if (eventType === 'message' && data) {
                                this.handleLine(data);
                            }
                        }
                    });

                    res.on('error', (err) => {
                        if (!resolved) { reject(err); }
                        else { this.emit('error', err); this.cleanupHttp(); }
                    });

                    res.on('close', () => {
                        this.emit('disconnected', 0);
                        this.cleanupHttp();
                    });
                },
            );

            req.on('error', (err) => { if (!resolved) { reject(err); } });
            this.sseReq = req;
        });
    }

    private async initialize(): Promise<void> {
        await this.request('initialize', {
            protocolVersion: '2024-11-05',
            capabilities: {},
            clientInfo: { name: 'vscode-caboose-mcp', version: '0.1.0' },
        });
        this.notify('notifications/initialized', {});
    }

    async listTools(): Promise<Tool[]> {
        const result = await this.request('tools/list', {}) as { tools?: Tool[] };
        return result.tools ?? [];
    }

    async callTool(name: string, args: Record<string, unknown>): Promise<string> {
        const result = await this.request('tools/call', { name, arguments: args }) as {
            content?: Array<{ type: string; text?: string }>;
            isError?: boolean;
        };
        const text = (result.content ?? [])
            .filter((c) => c.type === 'text')
            .map((c) => c.text ?? '')
            .join('\n');
        if (result.isError) throw new Error(text);
        return text;
    }

    disconnect(): void {
        this.cleanupPending();
        this.cleanupStdio();
        this.cleanupHttp();
    }

    dispose(): void {
        this.disconnect();
    }

    private request(method: string, params: unknown): Promise<unknown> {
        return new Promise((resolve, reject) => {
            if (!this.connected) {
                reject(new Error('Not connected'));
                return;
            }
            const id = this.nextId++;
            const timer = setTimeout(() => {
                this.pending.delete(id);
                reject(new Error(`Request "${method}" timed out after 30s`));
            }, 30_000);

            this.pending.set(id, { resolve, reject, timer });
            this.sendRaw(JSON.stringify({ jsonrpc: '2.0', id, method, params }));
        });
    }

    private notify(method: string, params: unknown): void {
        if (!this.connected) { return; }
        this.sendRaw(JSON.stringify({ jsonrpc: '2.0', method, params }));
    }

    /** Routes a raw JSON-RPC message to the active transport. */
    private sendRaw(msg: string): void {
        if (this.proc?.stdin) {
            this.proc.stdin.write(msg + '\n');
        } else if (this.messagesUrl) {
            const url = new URL(this.messagesUrl);
            const mod = url.protocol === 'https:' ? https : http;
            const body = Buffer.from(msg);
            const headers: Record<string, string | number> = {
                'Content-Type': 'application/json',
                'Content-Length': body.length,
                'Accept': 'application/json, text/event-stream',
            };
            if (this.sessionId) { headers['Mcp-Session-Id'] = this.sessionId; }

            const req = mod.request(
                { hostname: url.hostname, port: url.port || (url.protocol === 'https:' ? 443 : 80),
                  path: url.pathname + url.search, method: 'POST', headers },
                (res) => {
                    // Capture session ID for subsequent requests
                    const sid = res.headers['mcp-session-id'];
                    if (typeof sid === 'string' && !this.sessionId) { this.sessionId = sid; }

                    // Process response lines as they stream in (JSON or SSE)
                    let buf = '';
                    res.on('data', (chunk: Buffer) => {
                        buf += chunk.toString();
                        const parts = buf.split('\n');
                        buf = parts.pop() ?? '';
                        for (const line of parts) {
                            const s = line.startsWith('data: ') ? line.slice(6) : line;
                            if (s.trim()) { this.handleLine(s.trim()); }
                        }
                    });
                    res.on('end', () => {
                        const s = buf.startsWith('data: ') ? buf.slice(6) : buf;
                        if (s.trim()) { this.handleLine(s.trim()); }
                    });
                    res.on('error', (err) => this.emit('error', err));
                },
            );
            req.on('error', (err) => this.emit('error', err));
            req.write(body);
            req.end();
        }
    }

    private handleLine(line: string): void {
        const trimmed = line.trim();
        if (!trimmed) { return; }
        let msg: { id?: number; result?: unknown; error?: { message: string } };
        try { msg = JSON.parse(trimmed); } catch { return; }
        if (msg.id == null) { return; }
        const pending = this.pending.get(msg.id);
        if (!pending) { return; }
        clearTimeout(pending.timer);
        this.pending.delete(msg.id);
        if (msg.error) { pending.reject(new Error(msg.error.message)); }
        else { pending.resolve(msg.result); }
    }

    private cleanupPending(): void {
        for (const { reject, timer } of this.pending.values()) {
            clearTimeout(timer);
            reject(new Error('Connection closed'));
        }
        this.pending.clear();
    }

    private cleanupStdio(): void {
        this.proc?.kill();
        this.proc = null;
    }

    private cleanupHttp(): void {
        this.sseReq?.destroy();
        this.sseReq = null;
        this.messagesUrl = null;
        this.sessionId = null;
    }
}

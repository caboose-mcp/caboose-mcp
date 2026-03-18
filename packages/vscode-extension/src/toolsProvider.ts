import * as vscode from 'vscode';
import { McpClient, Tool } from './mcpClient';
import { isToolEnabled } from './config';

/** Maps a tool name prefix to a human-readable group label and VS Code codicon name. */
const GROUP_META: Record<string, { label: string; icon: string }> = {
    audit:      { label: 'Audit',        icon: 'shield' },
    approve:    { label: 'Audit',        icon: 'shield' },
    deny:       { label: 'Audit',        icon: 'shield' },
    calendar:   { label: 'Calendar',     icon: 'calendar' },
    chezmoi:    { label: 'Chezmoi',      icon: 'home' },
    claude:     { label: 'Claude',       icon: 'robot' },
    cloudsync:  { label: 'Cloud Sync',   icon: 'cloud' },
    postgres:   { label: 'Database',     icon: 'database' },
    mongodb:    { label: 'Database',     icon: 'database' },
    discord:    { label: 'Discord',      icon: 'comment-discussion' },
    docker:     { label: 'Docker',       icon: 'server-process' },
    focus:      { label: 'Focus',        icon: 'target' },
    github:     { label: 'GitHub',       icon: 'github' },
    greptile:   { label: 'Greptile',     icon: 'search' },
    health:     { label: 'Health',       icon: 'heart' },
    joke:       { label: 'Jokes',        icon: 'smiley' },
    dad:        { label: 'Jokes',        icon: 'smiley' },
    learn:      { label: 'Learning',     icon: 'book' },
    mermaid:    { label: 'Mermaid',      icon: 'type-hierarchy' },
    note:       { label: 'Notes',        icon: 'note' },
    notes:      { label: 'Notes',        icon: 'note' },
    agent:      { label: 'Persona',      icon: 'person' },
    blender:    { label: 'Printing',     icon: 'layers' },
    bambu:      { label: 'Printing',     icon: 'layers' },
    sandbox:    { label: 'Sandbox',      icon: 'beaker' },
    secret:     { label: 'Secrets',      icon: 'lock' },
    si:         { label: 'Self-Improve', icon: 'lightbulb' },
    setup:      { label: 'Setup',        icon: 'tools' },
    slack:      { label: 'Slack',        icon: 'comment' },
    source:     { label: 'Sources',      icon: 'rss' },
    execute:    { label: 'System',       icon: 'terminal' },
    tool:       { label: 'Toolsmith',    icon: 'wrench' },
};

/**
 * Returns the display group label for a tool, derived from its name prefix
 * (the portion before the first underscore). Falls back to the raw prefix if
 * not found in {@link GROUP_META}.
 */
function groupForTool(name: string): string {
    const prefix = name.split('_')[0];
    return GROUP_META[prefix]?.label ?? prefix;
}

/**
 * Returns the codicon name for a group label by looking it up in
 * {@link GROUP_META}. Defaults to `"symbol-misc"` for unknown groups.
 */
function iconForGroup(label: string): string {
    const entry = Object.values(GROUP_META).find((m) => m.label === label);
    return entry?.icon ?? 'symbol-misc';
}

/**
 * A leaf tree node representing a single MCP tool.
 *
 * Clicking the item triggers the `cabooseMcp.runTool` command with the
 * underlying {@link Tool} passed as an argument.
 */
export class ToolItem extends vscode.TreeItem {
    constructor(public readonly tool: Tool) {
        super(tool.name, vscode.TreeItemCollapsibleState.None);
        this.tooltip = tool.description;
        this.description = tool.description.length > 60
            ? tool.description.slice(0, 57) + '...'
            : tool.description;
        this.contextValue = 'tool';
        this.iconPath = new vscode.ThemeIcon('symbol-function');
        this.command = {
            command: 'cabooseMcp.runTool',
            title: 'Run',
            arguments: [tool],
        };
    }
}

/**
 * A collapsible tree node that groups related {@link ToolItem}s under a
 * shared category label (e.g. "Focus", "Slack").
 */
export class GroupItem extends vscode.TreeItem {
    constructor(
        public readonly label: string,
        public readonly tools: Tool[],
    ) {
        super(label, vscode.TreeItemCollapsibleState.Collapsed);
        this.description = `${tools.length} tool${tools.length !== 1 ? 's' : ''}`;
        this.contextValue = 'group';
        this.iconPath = new vscode.ThemeIcon(iconForGroup(label));
    }
}

type TreeNode = GroupItem | ToolItem;

/**
 * `TreeDataProvider` that populates the **Caboose MCP** sidebar panel.
 *
 * Tools are fetched from the connected {@link McpClient}, filtered by the
 * `enabledTools` allowlist, then bucketed into named groups based on their
 * name prefix. The provider shows a loading spinner while fetching, an error
 * node on failure, and an empty-state prompt when disconnected.
 */
export class ToolsProvider implements vscode.TreeDataProvider<TreeNode> {
    private _onChange = new vscode.EventEmitter<TreeNode | undefined | null>();
    readonly onDidChangeTreeData = this._onChange.event;

    private groups = new Map<string, Tool[]>();
    private loading = false;
    private errorMessage: string | null = null;

    constructor(private readonly client: McpClient) {}

    /**
     * Fetches all tools from the server, applies the `enabledTools` filter,
     * and rebuilds the group map. Fires a tree-data-changed event on both
     * start (to show the spinner) and completion.
     *
     * @param enabledTools Allowlist from workspace settings.
     */
    async loadTools(enabledTools: string[]): Promise<void> {
        this.loading = true;
        this.errorMessage = null;
        this._onChange.fire(undefined);

        try {
            const all = await this.client.listTools();
            const filtered = all.filter((t) => isToolEnabled(t.name, enabledTools));
            this.groups = new Map();

            for (const tool of filtered) {
                const group = groupForTool(tool.name);
                if (!this.groups.has(group)) this.groups.set(group, []);
                this.groups.get(group)!.push(tool);
            }
        } catch (err: unknown) {
            this.errorMessage = err instanceof Error ? err.message : String(err);
        } finally {
            this.loading = false;
            this._onChange.fire(undefined);
        }
    }

    /** Removes all groups and resets error state, e.g. after a disconnect. */
    clear(): void {
        this.groups.clear();
        this.errorMessage = null;
        this._onChange.fire(undefined);
    }

    /** Forces a tree-data-changed notification without re-fetching tools. */
    refresh(): void {
        this._onChange.fire(undefined);
    }

    /** Returns `element` unchanged — VS Code uses the object itself as the tree item. */
    getTreeItem(element: TreeNode): vscode.TreeItem {
        return element;
    }

    /**
     * Returns the children of a tree node.
     *
     * - Root (`element` is `undefined`): returns sorted {@link GroupItem}s, or
     *   a loading/error/empty placeholder.
     * - {@link GroupItem}: returns its tools sorted alphabetically as {@link ToolItem}s.
     * - {@link ToolItem}: returns an empty array (leaf node).
     */
    getChildren(element?: TreeNode): TreeNode[] {
        if (this.loading) {
            const loading = new vscode.TreeItem('Loading tools...');
            loading.iconPath = new vscode.ThemeIcon('loading~spin');
            return [loading as TreeNode];
        }

        if (this.errorMessage) {
            const err = new vscode.TreeItem(`Error: ${this.errorMessage}`);
            err.iconPath = new vscode.ThemeIcon('error');
            return [err as TreeNode];
        }

        if (!element) {
            if (this.groups.size === 0) {
                const empty = new vscode.TreeItem('Not connected — click Connect');
                empty.iconPath = new vscode.ThemeIcon('plug');
                return [empty as TreeNode];
            }
            return Array.from(this.groups.entries())
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([label, tools]) => new GroupItem(label, tools));
        }

        if (element instanceof GroupItem) {
            return element.tools
                .sort((a, b) => a.name.localeCompare(b.name))
                .map((t) => new ToolItem(t));
        }

        return [];
    }
}

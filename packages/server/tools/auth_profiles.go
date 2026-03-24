package tools

// auth_profiles.go — named tool profiles for JWT token issuance.
//
// Profiles provide a convenient way to issue tokens with predefined tool allowlists.
// Instead of manually specifying 20+ tool names, users can request a profile like
// "vscode" or "discord" and get the appropriate tool set automatically.

// ClientProfile maps a named profile to its allowed tool set.
// An empty Tools list means full access (admin equivalent).
var clientProfiles = map[string][]string{
	// vscode — full hosted surface for development productivity
	// Used by: VS Code, Claude Code, other dev IDEs
	"vscode": {
		// Core utilities
		"joke", "dad_joke",
		// File access (Claude API)
		"claude_read_file", "claude_write_file", "claude_append_memory", "claude_list_files",
		// Secrets management
		"secret_get", "secret_list", "secret_set",
		// GitHub integration
		"github_search_code", "github_list_repos", "github_create_pr",
		// Database access
		"postgres_query", "postgres_list_tables",
		"mongodb_query", "mongodb_list_collections",
		// Chat platforms (manage messages from API)
		"slack_post_message", "slack_read_messages", "slack_list_channels",
		"discord_post_message", "discord_read_messages", "discord_list_channels",
		"discord_webhook_post",
		// Environment
		"env_check", "env_fix",
		// Visualization
		"mermaid_generate",
		// Code search (Greptile)
		"greptile_query", "greptile_index",
		// Code improvement tools
		"si_scan_dir", "si_git_diff", "si_suggest", "si_list_pending",
		"si_approve", "si_apply", "si_reject", "si_tech_digest", "si_report_error",
		// Setup & configuration
		"setup_check", "setup_bot_configure", "setup_github_mcp_info",
		"setup_write_env", "setup_init_dirs",
		// Cloud sync
		"cloudsync_status", "cloudsync_push", "cloudsync_pull",
		"cloudsync_env_list", "cloudsync_env_set", "cloudsync_setup",
		// System health
		"health_report",
		// User profile
		"persona_get", "persona_set",
		// Sandbox execution
		"sandbox_run", "sandbox_list", "sandbox_diff", "sandbox_clean", "sandbox_suggestion",
		// Audit
		"audit_config", "audit_list", "audit_pending",
		// Authentication & tokens
		"auth_create_token", "auth_list_tokens", "auth_revoke_token",
		"auth_link_identity", "auth_list_identities", "auth_unlink_identity",
		"auth_list_profiles",
		// Knowledge sources
		"source_add", "source_list", "source_edit", "source_remove", "source_check", "source_digest",
		// Repository tools (manage MCP tools)
		"repo_create_tool", "repo_test_tool", "repo_approve_tool", "repo_reject_tool",
		"repo_list_pending_tools", "repo_deploy", "repo_sync_ui",
		// Deck generation (Gamma)
		"gamma_generate_deck", "gamma_list_decks", "gamma_update_deck",
		// Organization health & management
		"org_health_status", "org_health_refresh", "org_health_next_pr",
		"org_list_repos", "org_sync_status", "org_pr_dashboard", "org_pull_all", "org_branch_cleanup",
	},

	// discord — public-safe subset for Discord bot use
	// Used by: Discord chat bot (limited to read-only, non-sensitive operations)
	"discord": {
		"joke", "dad_joke",
		"health_report",
		"org_health_status", "org_health_refresh", "org_health_next_pr",
		"org_list_repos", "org_sync_status", "org_pr_dashboard",
	},

	// api — same as vscode for now; separate entry allows future narrowing
	// Used by: raw API clients, custom scripts
	// Currently maps to vscode; can be restricted in the future
	"api": nil,
}

// ToolsForProfile returns the allowed tool list for a named profile.
// Returns (tools, ok) where ok=false if the profile is unknown.
func ToolsForProfile(name string) ([]string, bool) {
	tools, ok := clientProfiles[name]
	if !ok {
		return nil, false
	}
	if name == "api" {
		// api currently maps to vscode profile
		return clientProfiles["vscode"], true
	}
	return tools, true
}

// ListProfiles returns a map of all profile names to their tool lists.
// Used by auth_list_profiles tool to display available profiles.
func ListProfiles() map[string][]string {
	out := make(map[string][]string, len(clientProfiles))
	for k, v := range clientProfiles {
		if k == "api" {
			out[k] = clientProfiles["vscode"]
		} else {
			out[k] = v
		}
	}
	return out
}

package tools

// secrets — GPG-encrypted secret storage.
//
// Secrets are stored as individual .gpg files under CLAUDE_DIR/secrets/.
// Encryption uses the public key identified by GPG_KEY_ID; decryption uses
// whatever key is available in the local GPG keyring (typically via gpg-agent).
//
// Requires:
//   - gpg installed and available in PATH
//   - GPG_KEY_ID env var set to a valid key ID or fingerprint
//
// Storage: CLAUDE_DIR/secrets/<name>.gpg
//
// Tools:
//   secret_set  — encrypt and store a secret value
//   secret_get  — decrypt and return a stored secret
//   secret_list — list names of all stored secrets (values never exposed)

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterSecrets(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("secret_set",
		mcp.WithDescription("Encrypt and store a secret value using GPG. Stored in CLAUDE_DIR/secrets/<name>.gpg"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Secret name (alphanumeric, dashes, underscores)")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Secret value to encrypt")),
	), secretSetHandler(cfg))

	s.AddTool(mcp.NewTool("secret_get",
		mcp.WithDescription("Decrypt and return a stored secret value."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Secret name")),
	), secretGetHandler(cfg))

	s.AddTool(mcp.NewTool("secret_list",
		mcp.WithDescription("List names of all stored secrets."),
	), secretListHandler(cfg))
}

func secretsDir(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "secrets")
}

func validateSecretName(name string) error {
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("secret name may only contain alphanumeric characters, dashes, and underscores")
		}
	}
	return nil
}

func secretSetHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if cfg.GPGKeyID == "" {
			return mcp.NewToolResultError("GPG_KEY_ID is not set. Set it to your GPG key ID to enable secrets."), nil
		}
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := validateSecretName(name); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		dir := secretsDir(cfg)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("mkdir error: %v", err)), nil
		}
		outPath := filepath.Join(dir, name+".gpg")
		cmd := exec.Command("gpg", "--batch", "--yes", "--recipient", cfg.GPGKeyID, "--encrypt", "--output", outPath)
		cmd.Stdin = strings.NewReader(value)
		if out, err := cmd.CombinedOutput(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gpg encrypt error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("secret '%s' stored", name)), nil
	}
}

func secretGetHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if cfg.GPGKeyID == "" {
			return mcp.NewToolResultError("GPG_KEY_ID is not set. Set it to your GPG key ID to enable secrets."), nil
		}
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := validateSecretName(name); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		inPath := filepath.Join(secretsDir(cfg), name+".gpg")
		cmd := exec.Command("gpg", "--batch", "--decrypt", inPath)
		out, err := cmd.Output()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("gpg decrypt error: %v", err)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}

func secretListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dir := secretsDir(cfg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return mcp.NewToolResultText("(no secrets stored)"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("readdir error: %v", err)), nil
		}
		var names []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".gpg") {
				names = append(names, strings.TrimSuffix(e.Name(), ".gpg"))
			}
		}
		if len(names) == 0 {
			return mcp.NewToolResultText("(no secrets stored)"), nil
		}
		return mcp.NewToolResultText(strings.Join(names, "\n")), nil
	}
}

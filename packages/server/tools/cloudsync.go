package tools

// Cloud config sync — encrypt and store caboose-mcp config remotely so you can
// restore it on any machine after cloning the repo and authenticating with GitHub.
//
// Encryption: AES-256-GCM with a key derived via scrypt from a user passphrase.
// Wire format (binary, base64-encoded for transport):
//   [1 byte version] [32 bytes salt] [12 bytes nonce] [ciphertext...]
//
// Storage backends:
//   gist  — private GitHub Gist (no extra auth: reuses GitHub token from `gh auth token`)
//   s3    — AWS S3 (uses `aws s3` CLI; requires CLOUDSYNC_S3_BUCKET + AWS credentials)
//
// What gets synced (the "config bundle"):
//   - All KEY=VALUE env vars from CLAUDE_DIR/cloudsync-env.json  (you populate this once)
//   - Sources list       (CLAUDE_DIR/sources/*.json)
//   - Selfimprove allowlist (CLAUDE_DIR/selfimprove-allowlist.json)
//   - Learning schedule  (CLAUDE_DIR/learning/schedule.json)
//   NOT synced: secrets (stay GPG-encrypted locally), learning session history, errors
//
// Sync state is persisted in CLAUDE_DIR/cloudsync.json:
//   { "backend": "gist", "gist_id": "...", "s3_path": "...", "last_push": "...", "last_pull": "..." }
//
// Tools:
//   cloudsync_setup  — interactive first-time setup: choose backend, write sync state
//   cloudsync_push   — encrypt bundle and upload to configured backend
//   cloudsync_pull   — download and decrypt bundle, write files to CLAUDE_DIR
//   cloudsync_status — show current sync config and last push/pull times
//   cloudsync_env_set — add/update an env var in the sync bundle
//   cloudsync_env_list — list env vars stored in the sync bundle (values redacted)

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// syncState is persisted to CLAUDE_DIR/cloudsync.json
type syncState struct {
	Backend  string `json:"backend"` // "gist" | "s3"
	GistID   string `json:"gist_id,omitempty"`
	S3Path   string `json:"s3_path,omitempty"` // s3://bucket/key
	LastPush string `json:"last_push,omitempty"`
	LastPull string `json:"last_pull,omitempty"`
}

// configBundle is what we encrypt and store
type configBundle struct {
	Version   int               `json:"version"`
	BundleAt  string            `json:"bundle_at"`
	EnvVars   map[string]string `json:"env_vars"` // from cloudsync-env.json
	Sources   []json.RawMessage `json:"sources"`
	Allowlist json.RawMessage   `json:"allowlist,omitempty"`
	Schedule  json.RawMessage   `json:"schedule,omitempty"`
}

func RegisterCloudSync(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("cloudsync_setup",
		mcp.WithDescription("First-time cloud sync setup. Provisions storage (creates S3 bucket or GitHub Gist) and writes sync state. Requires gh CLI (for gist) or aws CLI (for s3)."),
		mcp.WithString("backend", mcp.Required(), mcp.Description("Storage backend: gist (GitHub private Gist, uses `gh` CLI) or s3 (creates S3 bucket with `aws` CLI)")),
		mcp.WithString("s3_bucket", mcp.Description("S3 bucket name for s3 backend. If omitted, auto-generates 'caboose-mcp-config-<account-id>'. Created if it doesn't exist.")),
		mcp.WithString("s3_region", mcp.Description("AWS region for new bucket (default: us-east-1)")),
		mcp.WithString("s3_key", mcp.Description("S3 object key (default: caboose-mcp/config.enc)")),
		mcp.WithString("gist_id", mcp.Description("Existing Gist ID to reuse (leave empty to create a new one on first push)")),
	), cloudsyncSetupHandler(cfg))

	s.AddTool(mcp.NewTool("cloudsync_push",
		mcp.WithDescription("Encrypt the config bundle and upload it to the configured backend. Prompts for passphrase."),
		mcp.WithString("passphrase", mcp.Required(), mcp.Description("Encryption passphrase (used to derive AES-256 key via scrypt)")),
	), cloudsyncPushHandler(cfg))

	s.AddTool(mcp.NewTool("cloudsync_pull",
		mcp.WithDescription("Download and decrypt the config bundle, restoring files to CLAUDE_DIR. Safe to run on a fresh machine after cloning."),
		mcp.WithString("passphrase", mcp.Required(), mcp.Description("The passphrase used when pushing")),
		mcp.WithString("gist_id", mcp.Description("Gist ID to pull from (overrides saved state — useful on first pull on a new machine)")),
		mcp.WithString("s3_path", mcp.Description("S3 path to pull from, e.g. s3://bucket/key (overrides saved state)")),
	), cloudsyncPullHandler(cfg))

	s.AddTool(mcp.NewTool("cloudsync_status",
		mcp.WithDescription("Show current cloud sync configuration and last push/pull times."),
	), cloudsyncStatusHandler(cfg))

	s.AddTool(mcp.NewTool("cloudsync_env_set",
		mcp.WithDescription("Add or update an environment variable in the cloud sync bundle. These will be written to .env on pull."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Environment variable name, e.g. SLACK_TOKEN")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value")),
	), cloudsyncEnvSetHandler(cfg))

	s.AddTool(mcp.NewTool("cloudsync_env_list",
		mcp.WithDescription("List env var keys stored in the sync bundle. Values are redacted for safety."),
	), cloudsyncEnvListHandler(cfg))
}

// ---- paths ----

func syncStatePath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "cloudsync.json")
}

func syncEnvPath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "cloudsync-env.json")
}

// ---- state helpers ----

func loadSyncState(cfg *config.Config) (syncState, error) {
	var s syncState
	data, err := os.ReadFile(syncStatePath(cfg))
	if err != nil {
		return s, fmt.Errorf("no cloud sync configured — run cloudsync_setup first")
	}
	return s, json.Unmarshal(data, &s)
}

func saveSyncState(cfg *config.Config, s syncState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(syncStatePath(cfg), data, 0600)
}

// ---- env store helpers ----

func loadSyncEnv(cfg *config.Config) map[string]string {
	data, err := os.ReadFile(syncEnvPath(cfg))
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func saveSyncEnv(cfg *config.Config, env map[string]string) error {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(syncEnvPath(cfg), data, 0600)
}

// ---- encryption ----

const encVersion byte = 1

// deriveKey uses scrypt to produce a 32-byte AES key from passphrase + salt.
func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	// scrypt params: N=32768, r=8, p=1 — balanced for interactive use
	return scrypt.Key([]byte(passphrase), salt, 32768, 8, 1, 32)
}

func encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	var out bytes.Buffer
	out.WriteByte(encVersion)
	out.Write(salt)
	out.Write(nonce)
	out.Write(ciphertext)
	return out.Bytes(), nil
}

func decrypt(data []byte, passphrase string) ([]byte, error) {
	if len(data) < 1+32+12+16 {
		return nil, fmt.Errorf("ciphertext too short")
	}
	if data[0] != encVersion {
		return nil, fmt.Errorf("unsupported encryption version %d", data[0])
	}
	salt := data[1:33]
	nonce := data[33:45]
	ciphertext := data[45:]

	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// ---- bundle builder ----

func buildBundle(cfg *config.Config) ([]byte, error) {
	bundle := configBundle{
		Version:  1,
		BundleAt: time.Now().Format(time.RFC3339),
		EnvVars:  loadSyncEnv(cfg),
	}

	// Sources
	entries, _ := os.ReadDir(sourcesDir(cfg))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourcesDir(cfg), e.Name()))
		if err == nil {
			bundle.Sources = append(bundle.Sources, json.RawMessage(data))
		}
	}

	// Allowlist
	if data, err := os.ReadFile(filepath.Join(cfg.ClaudeDir, "selfimprove-allowlist.json")); err == nil {
		bundle.Allowlist = json.RawMessage(data)
	}

	// Learning schedule
	if data, err := os.ReadFile(filepath.Join(cfg.ClaudeDir, "learning", "schedule.json")); err == nil {
		bundle.Schedule = json.RawMessage(data)
	}

	return json.MarshalIndent(bundle, "", "  ")
}

// ---- bundle restorer ----

func restoreBundle(cfg *config.Config, plaintext []byte, dryRun bool) (string, error) {
	var bundle configBundle
	if err := json.Unmarshal(plaintext, &bundle); err != nil {
		return "", fmt.Errorf("invalid bundle: %v", err)
	}

	var restored []string

	// Write .env file
	if len(bundle.EnvVars) > 0 {
		var lines []string
		for k, v := range bundle.EnvVars {
			lines = append(lines, k+"="+v)
		}
		envPath := filepath.Join(filepath.Dir(cfg.ClaudeDir), ".env")
		if !dryRun {
			if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
				return "", fmt.Errorf("writing .env: %w", err)
			}
		}
		restored = append(restored, fmt.Sprintf(".env (%d vars)", len(bundle.EnvVars)))
	}

	// Write sources
	if len(bundle.Sources) > 0 {
		if !dryRun {
			if err := os.MkdirAll(sourcesDir(cfg), 0755); err != nil {
				return "", fmt.Errorf("creating sources directory: %w", err)
			}
			for _, raw := range bundle.Sources {
				var src Source
				if err := json.Unmarshal(raw, &src); err == nil && src.ID != "" {
					if err := os.WriteFile(filepath.Join(sourcesDir(cfg), src.ID+".json"), raw, 0644); err != nil {
						return "", fmt.Errorf("writing source %s: %w", src.ID, err)
					}
				}
			}
		}
		restored = append(restored, fmt.Sprintf("%d source(s)", len(bundle.Sources)))
	}

	// Write allowlist
	if bundle.Allowlist != nil {
		if !dryRun {
			if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "selfimprove-allowlist.json"), bundle.Allowlist, 0644); err != nil {
				return "", fmt.Errorf("writing selfimprove-allowlist.json: %w", err)
			}
		}
		restored = append(restored, "selfimprove-allowlist.json")
	}

	// Write schedule
	if bundle.Schedule != nil {
		if !dryRun {
			if err := os.MkdirAll(filepath.Join(cfg.ClaudeDir, "learning"), 0755); err != nil {
				return "", fmt.Errorf("creating learning directory: %w", err)
			}
			if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "learning", "schedule.json"), bundle.Schedule, 0644); err != nil {
				return "", fmt.Errorf("writing learning/schedule.json: %w", err)
			}
		}
		restored = append(restored, "learning/schedule.json")
	}

	prefix := "Would restore"
	if !dryRun {
		prefix = "Restored"
	}
	return fmt.Sprintf("Bundle from %s\n%s: %s", bundle.BundleAt, prefix, strings.Join(restored, ", ")), nil
}

// ---- GitHub Gist backend ----

func gistPush(cfg *config.Config, encoded string, existingGistID string) (string, error) {
	if cfg.GitHubToken == "" {
		return "", fmt.Errorf("no GitHub token available — run `gh auth login` or set GITHUB_TOKEN")
	}

	filename := "caboose-mcp-config.enc"
	files := map[string]map[string]string{
		filename: {"content": encoded},
	}
	payload := map[string]any{
		"description": "caboose-mcp encrypted config — do not edit manually",
		"public":      false,
		"files":       files,
	}
	body, _ := json.Marshal(payload)

	var method, url string
	if existingGistID != "" {
		method = "PATCH"
		url = "https://api.github.com/gists/" + existingGistID
	} else {
		method = "POST"
		url = "https://api.github.com/gists"
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respData, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, respData)
	}

	var gist map[string]any
	if err := json.Unmarshal(respData, &gist); err != nil {
		return "", fmt.Errorf("failed to parse GitHub API response: %v", err)
	}
	id, _ := gist["id"].(string)
	return id, nil
}

func gistPull(cfg *config.Config, gistID string) (string, error) {
	if cfg.GitHubToken == "" {
		return "", fmt.Errorf("no GitHub token available — run `gh auth login` or set GITHUB_TOKEN")
	}

	req, err := http.NewRequest("GET", "https://api.github.com/gists/"+gistID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respData, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, respData)
	}

	var gist map[string]any
	if err := json.Unmarshal(respData, &gist); err != nil {
		return "", fmt.Errorf("failed to parse GitHub API response: %v", err)
	}

	files, _ := gist["files"].(map[string]any)
	for _, v := range files {
		fileObj, _ := v.(map[string]any)
		content, _ := fileObj["content"].(string)
		if content != "" {
			return content, nil
		}
	}
	return "", fmt.Errorf("gist %s has no file content", gistID)
}

// ---- S3 backend (via aws CLI) ----

func s3Push(s3Path, localFile string) error {
	out, err := exec.Command("aws", "s3", "cp", localFile, s3Path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("aws s3 cp error: %v\n%s", err, out)
	}
	return nil
}

func s3Pull(s3Path, localFile string) error {
	out, err := exec.Command("aws", "s3", "cp", s3Path, localFile).CombinedOutput()
	if err != nil {
		return fmt.Errorf("aws s3 cp error: %v\n%s", err, out)
	}
	return nil
}

// ---- tool handlers ----

func cloudsyncSetupHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		backend, err := req.RequireString("backend")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if backend != "gist" && backend != "s3" {
			return mcp.NewToolResultError("backend must be 'gist' or 's3'"), nil
		}

		state := syncState{Backend: backend}
		var log []string

		switch backend {

		case "gist":
			// Ensure gh is authenticated
			if cfg.GitHubToken == "" {
				// Try gh auth login interactively (works in terminal, not useful over stdio)
				out, err := exec.Command("gh", "auth", "status").CombinedOutput()
				if err != nil {
					return mcp.NewToolResultError(
						"GitHub CLI not authenticated.\n" +
							"Run:  gh auth login\n" +
							"Then re-run cloudsync_setup.",
					), nil
				}
				log = append(log, "gh auth status: "+strings.TrimSpace(string(out)))
				// Reload token
				if tokenOut, err := exec.Command("gh", "auth", "token").Output(); err == nil {
					cfg.GitHubToken = strings.TrimSpace(string(tokenOut))
				}
			}
			if cfg.GitHubToken == "" {
				return mcp.NewToolResultError("Still no GitHub token after auth check — run `gh auth login` manually"), nil
			}
			log = append(log, "GitHub token: OK")

			state.GistID = req.GetString("gist_id", "")
			if state.GistID != "" {
				log = append(log, fmt.Sprintf("Using existing Gist: %s", state.GistID))
			} else {
				log = append(log, "A new private Gist will be created on first cloudsync_push.")
			}

		case "s3":
			// Verify aws CLI
			if _, err := exec.LookPath("aws"); err != nil {
				return mcp.NewToolResultError("aws CLI not found — install the AWS CLI and configure credentials with `aws configure`"), nil
			}

			// Verify credentials work
			whoami, err := exec.Command("aws", "sts", "get-caller-identity", "--output", "json").Output()
			if err != nil {
				return mcp.NewToolResultError(
					"AWS credentials not configured or invalid.\n" +
						"Run:  aws configure\n" +
						"Then re-run cloudsync_setup.",
				), nil
			}
			var identity map[string]any
			if err := json.Unmarshal(whoami, &identity); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to parse AWS identity: %v", err)), nil
			}
			accountID, _ := identity["Account"].(string)
			userARN, _ := identity["Arn"].(string)
			log = append(log, fmt.Sprintf("AWS identity: %s (account %s)", userARN, accountID))

			// Determine bucket name
			bucket := req.GetString("s3_bucket", "")
			if bucket == "" && accountID != "" {
				bucket = "caboose-mcp-config-" + accountID
			}
			if bucket == "" {
				return mcp.NewToolResultError("could not determine account ID — pass s3_bucket explicitly"), nil
			}

			region := req.GetString("s3_region", "us-east-1")
			key := req.GetString("s3_key", "caboose-mcp/config.enc")

			// Create bucket if it doesn't exist
			checkOut, checkErr := exec.Command("aws", "s3api", "head-bucket", "--bucket", bucket).CombinedOutput()
			if checkErr != nil {
				// Bucket doesn't exist — create it
				var createArgs []string
				if region == "us-east-1" {
					// us-east-1 must NOT include CreateBucketConfiguration
					createArgs = []string{"s3api", "create-bucket", "--bucket", bucket}
				} else {
					createArgs = []string{"s3api", "create-bucket", "--bucket", bucket,
						"--create-bucket-configuration", "LocationConstraint=" + region,
						"--region", region}
				}
				createOut, createErr := exec.Command("aws", createArgs...).CombinedOutput()
				if createErr != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to create bucket %s: %v\n%s", bucket, createErr, createOut)), nil
				}
				log = append(log, fmt.Sprintf("Created S3 bucket: %s (region: %s)", bucket, region))

				// Block public access
				exec.Command("aws", "s3api", "put-public-access-block",
					"--bucket", bucket,
					"--public-access-block-configuration",
					"BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true",
				).Run()
				log = append(log, "Bucket public access: blocked")

				// Enable versioning (keeps history of pushes)
				exec.Command("aws", "s3api", "put-bucket-versioning",
					"--bucket", bucket,
					"--versioning-configuration", "Status=Enabled",
				).Run()
				log = append(log, "Bucket versioning: enabled")
			} else {
				log = append(log, fmt.Sprintf("Using existing S3 bucket: %s", bucket))
				_ = checkOut
			}

			state.S3Path = "s3://" + bucket + "/" + key
			log = append(log, fmt.Sprintf("S3 path: %s", state.S3Path))
		}

		if err := os.MkdirAll(cfg.ClaudeDir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("mkdir: %v", err)), nil
		}
		if err := saveSyncState(cfg, state); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save state: %v", err)), nil
		}

		var msg strings.Builder
		msg.WriteString(fmt.Sprintf("Cloud sync configured: backend=%s\n\n", backend))
		msg.WriteString("Setup log:\n")
		for _, l := range log {
			msg.WriteString("  " + l + "\n")
		}
		msg.WriteString("\nNext steps:\n")
		msg.WriteString("  1. Add env vars:  cloudsync_env_set key=SLACK_TOKEN value=xoxb-...\n")
		msg.WriteString("  2. Push config:   cloudsync_push passphrase=<your-passphrase>\n")
		msg.WriteString("  3. On new machine:\n")
		msg.WriteString("       git clone https://github.com/caboose-mcp/server\n")
		msg.WriteString("       export PATH=$PATH:/usr/local/go/bin && go build -o caboose-mcp .\n")
		switch backend {
		case "gist":
			msg.WriteString(fmt.Sprintf("       gh auth login\n"))
			msg.WriteString(fmt.Sprintf("       # Then in Claude: cloudsync_pull passphrase=<passphrase> gist_id=<id>\n"))
		case "s3":
			msg.WriteString("       aws configure\n")
			msg.WriteString(fmt.Sprintf("       # Then in Claude: cloudsync_pull passphrase=<passphrase> s3_path=%s\n", state.S3Path))
		}
		return mcp.NewToolResultText(msg.String()), nil
	}
}

func cloudsyncPushHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		passphrase, err := req.RequireString("passphrase")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		state, err := loadSyncState(cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Build and encrypt
		plaintext, err := buildBundle(cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bundle error: %v", err)), nil
		}
		ciphertext, err := encrypt(plaintext, passphrase)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("encrypt error: %v", err)), nil
		}
		encoded := base64.StdEncoding.EncodeToString(ciphertext)

		var location string
		switch state.Backend {
		case "gist":
			gistID, err := gistPush(cfg, encoded, state.GistID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("gist push error: %v", err)), nil
			}
			state.GistID = gistID
			location = "Gist ID: " + gistID
		case "s3":
			// Write to temp file, then upload
			tmp, err := os.CreateTemp("", "caboose-sync-*.enc")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("temp file: %v", err)), nil
			}
			defer os.Remove(tmp.Name())
			tmp.WriteString(encoded)
			tmp.Close()
			if err := s3Push(state.S3Path, tmp.Name()); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			location = state.S3Path
		default:
			return mcp.NewToolResultError("unknown backend — run cloudsync_setup"), nil
		}

		state.LastPush = time.Now().Format(time.RFC3339)
		saveSyncState(cfg, state)

		return mcp.NewToolResultText(fmt.Sprintf(
			"Config pushed successfully.\n%s\n\nOn a new machine:\n  cloudsync_pull passphrase=<passphrase> gist_id=%s",
			location, state.GistID,
		)), nil
	}
}

func cloudsyncPullHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		passphrase, err := req.RequireString("passphrase")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Allow overriding backend location on first pull (new machine has no state file yet)
		overrideGistID := req.GetString("gist_id", "")
		overrideS3Path := req.GetString("s3_path", "")

		var state syncState
		savedState, stateErr := loadSyncState(cfg)
		if stateErr == nil {
			state = savedState
		}

		// Overrides take precedence
		if overrideGistID != "" {
			state.Backend = "gist"
			state.GistID = overrideGistID
		}
		if overrideS3Path != "" {
			state.Backend = "s3"
			state.S3Path = overrideS3Path
		}

		if state.Backend == "" {
			return mcp.NewToolResultError("no backend configured — pass gist_id or s3_path, or run cloudsync_setup"), nil
		}

		var encoded string
		switch state.Backend {
		case "gist":
			if state.GistID == "" {
				return mcp.NewToolResultError("no Gist ID — pass gist_id=<id>"), nil
			}
			encoded, err = gistPull(cfg, state.GistID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("gist pull error: %v", err)), nil
			}
		case "s3":
			tmp, err := os.CreateTemp("", "caboose-sync-*.enc")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("temp file: %v", err)), nil
			}
			tmpName := tmp.Name()
			tmp.Close()
			defer os.Remove(tmpName)
			if err := s3Pull(state.S3Path, tmpName); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			data, err := os.ReadFile(tmpName)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("read temp: %v", err)), nil
			}
			encoded = string(data)
		default:
			return mcp.NewToolResultError("unknown backend"), nil
		}

		ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("base64 decode: %v", err)), nil
		}
		plaintext, err := decrypt(ciphertext, passphrase)
		if err != nil {
			return mcp.NewToolResultError("decryption failed — wrong passphrase or corrupted data"), nil
		}

		os.MkdirAll(cfg.ClaudeDir, 0755)
		summary, err := restoreBundle(cfg, plaintext, false)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("restore error: %v", err)), nil
		}

		state.LastPull = time.Now().Format(time.RFC3339)
		saveSyncState(cfg, state)

		return mcp.NewToolResultText(summary + "\n\nRestart caboose-mcp (or reload Claude) to pick up new env vars from .env"), nil
	}
}

func cloudsyncStatusHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		state, err := loadSyncState(cfg)
		if err != nil {
			return mcp.NewToolResultText("Cloud sync not configured — run cloudsync_setup to get started."), nil
		}

		env := loadSyncEnv(cfg)
		sources, _ := allSources(cfg)

		var lines []string
		lines = append(lines, "=== Cloud Sync Status ===")
		lines = append(lines, fmt.Sprintf("Backend:    %s", state.Backend))
		switch state.Backend {
		case "gist":
			gistID := state.GistID
			if gistID == "" {
				gistID = "(none yet — will be created on first push)"
			}
			lines = append(lines, fmt.Sprintf("Gist ID:    %s", gistID))
		case "s3":
			lines = append(lines, fmt.Sprintf("S3 path:    %s", state.S3Path))
		}
		if state.LastPush != "" {
			lines = append(lines, fmt.Sprintf("Last push:  %s", state.LastPush))
		} else {
			lines = append(lines, "Last push:  (never)")
		}
		if state.LastPull != "" {
			lines = append(lines, fmt.Sprintf("Last pull:  %s", state.LastPull))
		} else {
			lines = append(lines, "Last pull:  (never)")
		}
		lines = append(lines, fmt.Sprintf("\nBundle would include:"))
		lines = append(lines, fmt.Sprintf("  %d env var(s)", len(env)))
		lines = append(lines, fmt.Sprintf("  %d source(s)", len(sources)))
		if _, err := os.Stat(filepath.Join(cfg.ClaudeDir, "selfimprove-allowlist.json")); err == nil {
			lines = append(lines, "  selfimprove-allowlist.json")
		}
		if _, err := os.Stat(filepath.Join(cfg.ClaudeDir, "learning", "schedule.json")); err == nil {
			lines = append(lines, "  learning/schedule.json")
		}

		if cfg.GitHubToken != "" {
			lines = append(lines, "\nGitHub auth: OK")
		} else {
			lines = append(lines, "\nGitHub auth: NOT available (run `gh auth login`)")
		}

		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}
}

func cloudsyncEnvSetHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		value, err := req.RequireString("value")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		env := loadSyncEnv(cfg)
		env[key] = value
		if err := saveSyncEnv(cfg, env); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("set %s in sync bundle (%d total vars)", key, len(env))), nil
	}
}

func cloudsyncEnvListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		env := loadSyncEnv(cfg)
		if len(env) == 0 {
			return mcp.NewToolResultText("(no env vars in sync bundle — use cloudsync_env_set to add them)"), nil
		}
		var lines []string
		for k := range env {
			lines = append(lines, fmt.Sprintf("  %s=<redacted>", k))
		}
		return mcp.NewToolResultText(fmt.Sprintf("%d env var(s) in sync bundle:\n%s", len(lines), strings.Join(lines, "\n"))), nil
	}
}

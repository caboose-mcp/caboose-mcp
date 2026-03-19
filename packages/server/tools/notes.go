package tools

// notes — quick markdown notes with optional Google Drive backup.
//
// Notes are stored locally at CLAUDE_DIR/notes.md (append-only log format).
// Google Drive backup uses the same OAuth2 credentials as calendar tools.
// Call calendar_auth_url first if you haven't already authorized Google.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	driveAPIv3       = "https://www.googleapis.com/drive/v3"
	driveUploadAPIv3 = "https://www.googleapis.com/upload/drive/v3"
	driveNotesFile   = "caboose-notes.md"
	driveMimePlain   = "text/plain"
)

func RegisterNotes(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("note_add",
		mcp.WithDescription("Append a quick note to CLAUDE_DIR/notes.md with a timestamp."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Note content")),
		mcp.WithString("tag", mcp.Description("Optional tag/category (e.g. 'idea', 'todo', 'bug')")),
	), noteAddHandler(cfg))

	s.AddTool(mcp.NewTool("note_list",
		mcp.WithDescription("List recent notes from CLAUDE_DIR/notes.md."),
		mcp.WithString("tag", mcp.Description("Filter by tag (optional)")),
	), noteListHandler(cfg))

	s.AddTool(mcp.NewTool("notes_drive_backup",
		mcp.WithDescription("Upload the local notes.md to Google Drive as 'caboose-notes.md'. Requires Google Calendar auth (calendar_auth_url)."),
	), notesDriveBackupHandler(cfg))

	s.AddTool(mcp.NewTool("notes_drive_restore",
		mcp.WithDescription("Download 'caboose-notes.md' from Google Drive, overwriting the local notes.md."),
	), notesDriveRestoreHandler(cfg))
}

func notesPath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "notes.md")
}

func noteAddHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, err := req.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		tag := req.GetString("tag", "")
		ts := time.Now().Format("2006-01-02 15:04")
		line := fmt.Sprintf("- [%s]", ts)
		if tag != "" {
			line += fmt.Sprintf(" #%s", tag)
		}
		line += " " + text + "\n"
		f, err := os.OpenFile(notesPath(cfg), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer f.Close()
		if _, err := f.WriteString(line); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("Note saved."), nil
	}
}

func noteListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filter := req.GetString("tag", "")
		data, err := os.ReadFile(notesPath(cfg))
		if os.IsNotExist(err) {
			return mcp.NewToolResultText("No notes yet."), nil
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if filter != "" {
			var filtered []string
			for _, l := range lines {
				if strings.Contains(l, "#"+filter) {
					filtered = append(filtered, l)
				}
			}
			lines = filtered
		}
		if len(lines) == 0 {
			return mcp.NewToolResultText("No notes found."), nil
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}
}

func notesDriveBackupHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := os.ReadFile(notesPath(cfg))
		if os.IsNotExist(err) {
			return mcp.NewToolResultText("No notes to backup."), nil
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, err := googleCalendarClient(cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Google Drive backup unavailable: %v", err)), nil
		}
	fileID, err := driveFindFile(client, driveNotesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Drive lookup unavailable: %v", err)), nil
	}
	if fileID == "" {
		if err := driveCreateFile(client, driveNotesFile, data); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Drive upload unavailable: %v", err)), nil
		}
	} else {
		if err := driveUpdateFile(client, fileID, data); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Drive update unavailable: %v", err)), nil
		}
	}
	return mcp.NewToolResultText(fmt.Sprintf("Notes backed up to Google Drive as %q (%d bytes).", driveNotesFile, len(data))), nil
	}
}

func notesDriveRestoreHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := googleCalendarClient(cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Google Drive restore unavailable: %v", err)), nil
		}
	fileID, err := driveFindFile(client, driveNotesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Drive lookup unavailable: %v", err)), nil
	}
	if fileID == "" {
		return mcp.NewToolResultText(fmt.Sprintf("No %q found on Drive. Run notes_drive_backup first.", driveNotesFile)), nil
	}
	data, err := driveDownloadFileByID(client, fileID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Drive download unavailable: %v", err)), nil
	}
		if err := os.WriteFile(notesPath(cfg), data, 0600); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Notes restored from Drive (%d bytes).", len(data))), nil
	}
}

// ---- Drive helpers ----

func driveFindFile(client *http.Client, name string) (string, error) {
q := url.QueryEscape(fmt.Sprintf("name='%s' and trashed=false", name))
resp, err := client.Get(fmt.Sprintf("%s/files?q=%s&fields=files(id)", driveAPIv3, q))
if err != nil {
return "", err
}
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)
if resp.StatusCode >= 400 {
return "", fmt.Errorf("Drive API error (HTTP %d): %s", resp.StatusCode, body)
}
var list struct {
Files []struct {
ID string `json:"id"`
} `json:"files"`
}
if err := json.Unmarshal(body, &list); err != nil {
return "", err
}
if len(list.Files) == 0 {
return "", nil
}
return list.Files[0].ID, nil
}

func driveCreateFile(client *http.Client, name string, data []byte) error {
	boundary := fmt.Sprintf("bound%d", time.Now().UnixNano())
	meta, _ := json.Marshal(map[string]string{"name": name})
	body := fmt.Sprintf("--%s\r\nContent-Type: application/json\r\n\r\n%s\r\n--%s\r\nContent-Type: %s\r\n\r\n%s\r\n--%s--",
		boundary, meta, boundary, driveMimePlain, data, boundary)
	req, _ := http.NewRequest("POST", driveUploadAPIv3+"/files?uploadType=multipart", strings.NewReader(body))
	req.Header.Set("Content-Type", fmt.Sprintf("multipart/related; boundary=%s", boundary))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

func driveUpdateFile(client *http.Client, fileID string, data []byte) error {
	req, _ := http.NewRequest("PATCH",
		fmt.Sprintf("%s/files/%s?uploadType=media", driveUploadAPIv3, fileID),
		strings.NewReader(string(data)))
	req.Header.Set("Content-Type", driveMimePlain)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

func driveDownloadFileByID(client *http.Client, fileID string) ([]byte, error) {
	resp, err := client.Get(fmt.Sprintf("%s/files/%s?alt=media", driveAPIv3, fileID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	return io.ReadAll(resp.Body)
}

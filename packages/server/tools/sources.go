package tools

// Source management — watch GitHub repos/users, RSS feeds, URLs, npm/PyPI packages.
//
// Storage: CLAUDE_DIR/sources/<id>.json  (one file per source)
//          CLAUDE_DIR/sources/.seen/<id>  (last-seen marker per source)
//
// Supported types:
//   github_repo  — watch commits, releases, or issues on a repo (owner/repo)
//   github_user  — watch a GitHub user's public activity
//   rss          — any RSS or Atom feed URL
//   url          — generic URL; detects content changes by hash
//   npm          — npm package; checks latest version via registry API
//   pypi         — PyPI package; checks latest version via JSON API
//
// Tools:
//   source_add     — add a new source
//   source_list    — list sources, optionally filtered by type or tag
//   source_edit    — update name, url, tags, description, or watch mode
//   source_remove  — delete a source
//   source_check   — check one or all sources for new activity since last check
//   source_digest  — run source_check and post a digest to Slack/Discord

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Source represents a watched resource.
type Source struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"` // github_repo | github_user | rss | url | npm | pypi
	URL         string    `json:"url"`  // canonical URL or identifier (e.g. "owner/repo" for github)
	Tags        []string  `json:"tags"`
	Description string    `json:"description"`
	WatchMode   string    `json:"watch_mode"` // for github_repo: commits | releases | issues | all
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CheckResult is returned by source_check.
type CheckResult struct {
	SourceID   string   `json:"source_id"`
	SourceName string   `json:"source_name"`
	Type       string   `json:"type"`
	HasUpdates bool     `json:"has_updates"`
	Summary    string   `json:"summary"`
	Items      []string `json:"items,omitempty"`
	Error      string   `json:"error,omitempty"`
	CheckedAt  string   `json:"checked_at"`
}

func RegisterSources(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("source_add",
		mcp.WithDescription("Add a source to watch. Types: github_repo, github_user, rss, url, npm, pypi"),
		mcp.WithString("type", mcp.Required(), mcp.Description("Source type: github_repo | github_user | rss | url | npm | pypi")),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL or identifier (e.g. 'torvalds/linux' for github_repo, package name for npm/pypi, full URL for rss/url)")),
		mcp.WithString("name", mcp.Description("Human-friendly label (auto-derived from url if omitted)")),
		mcp.WithString("tags", mcp.Description("Comma-separated tags e.g. 'rust,security,ai'")),
		mcp.WithString("description", mcp.Description("What to watch for / why this source matters")),
		mcp.WithString("watch_mode", mcp.Description("For github_repo: commits | releases | issues | all (default: commits)")),
	), sourceAddHandler(cfg))

	s.AddTool(mcp.NewTool("source_list",
		mcp.WithDescription("List watched sources."),
		mcp.WithString("type", mcp.Description("Filter by type (optional)")),
		mcp.WithString("tag", mcp.Description("Filter by tag (optional)")),
	), sourceListHandler(cfg))

	s.AddTool(mcp.NewTool("source_edit",
		mcp.WithDescription("Edit an existing source by ID."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Source ID")),
		mcp.WithString("name", mcp.Description("New name")),
		mcp.WithString("url", mcp.Description("New URL/identifier")),
		mcp.WithString("tags", mcp.Description("New comma-separated tags (replaces existing)")),
		mcp.WithString("description", mcp.Description("New description")),
		mcp.WithString("watch_mode", mcp.Description("New watch mode (for github_repo)")),
	), sourceEditHandler(cfg))

	s.AddTool(mcp.NewTool("source_remove",
		mcp.WithDescription("Remove a watched source by ID."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Source ID")),
	), sourceRemoveHandler(cfg))

	s.AddTool(mcp.NewTool("source_check",
		mcp.WithDescription("Check one or all sources for new activity since last check. Returns a summary of updates."),
		mcp.WithString("id", mcp.Description("Check a specific source ID (omit to check all)")),
		mcp.WithBoolean("force", mcp.Description("Ignore last-seen state and report all current items")),
	), sourceCheckHandler(cfg))

	s.AddTool(mcp.NewTool("source_digest",
		mcp.WithDescription("Check all sources and post a digest of updates to Slack and/or Discord."),
		mcp.WithBoolean("post_slack", mcp.Description("Post to Slack")),
		mcp.WithString("slack_channel", mcp.Description("Slack channel ID or name")),
		mcp.WithBoolean("post_discord", mcp.Description("Post to Discord")),
		mcp.WithString("discord_channel_id", mcp.Description("Discord channel ID")),
	), sourceDigestHandler(cfg))
}

// ---- storage helpers ----

func sourcesDir(cfg *config.Config) string { return filepath.Join(cfg.ClaudeDir, "sources") }
func seenDir(cfg *config.Config) string    { return filepath.Join(cfg.ClaudeDir, "sources", ".seen") }

func saveSource(cfg *config.Config, src Source) error {
	dir := sourcesDir(cfg)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, src.ID+".json"), data, 0644)
}

func loadSource(cfg *config.Config, id string) (Source, error) {
	var src Source
	data, err := os.ReadFile(filepath.Join(sourcesDir(cfg), id+".json"))
	if err != nil {
		return src, fmt.Errorf("source %q not found", id)
	}
	return src, json.Unmarshal(data, &src)
}

func allSources(cfg *config.Config) ([]Source, error) {
	entries, err := os.ReadDir(sourcesDir(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sources []Source
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		src, err := loadSource(cfg, id)
		if err == nil {
			sources = append(sources, src)
		}
	}
	return sources, nil
}

func readSeen(cfg *config.Config, id string) string {
	data, err := os.ReadFile(filepath.Join(seenDir(cfg), id))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeSeen(cfg *config.Config, id, value string) {
	os.MkdirAll(seenDir(cfg), 0755)
	os.WriteFile(filepath.Join(seenDir(cfg), id), []byte(value), 0644)
}

// ---- source_add ----

var slugRe = regexp.MustCompile(`[^a-z0-9_-]`)

func deriveID(srcType, url string) string {
	combined := srcType + "_" + url
	combined = strings.ToLower(combined)
	combined = slugRe.ReplaceAllString(combined, "_")
	if len(combined) > 48 {
		h := sha256.Sum256([]byte(combined))
		combined = fmt.Sprintf("%s_%x", combined[:32], h[:4])
	}
	return combined
}

func deriveName(srcType, url string) string {
	switch srcType {
	case "github_repo", "github_user":
		return url
	case "npm", "pypi":
		return url
	default:
		// strip scheme for rss/url
		name := strings.TrimPrefix(url, "https://")
		name = strings.TrimPrefix(name, "http://")
		if len(name) > 60 {
			name = name[:60] + "…"
		}
		return name
	}
}

func parseTags(raw string) []string {
	if raw == "" {
		return nil
	}
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func validateType(t string) error {
	valid := map[string]bool{
		"github_repo": true, "github_user": true,
		"rss": true, "url": true, "npm": true, "pypi": true,
	}
	if !valid[t] {
		return fmt.Errorf("unknown type %q; valid types: github_repo, github_user, rss, url, npm, pypi", t)
	}
	return nil
}

func sourceAddHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		srcType, err := req.RequireString("type")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := validateType(srcType); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		url, err := req.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		name := req.GetString("name", deriveName(srcType, url))
		tags := parseTags(req.GetString("tags", ""))
		description := req.GetString("description", "")
		watchMode := req.GetString("watch_mode", "commits")

		src := Source{
			ID:          deriveID(srcType, url),
			Name:        name,
			Type:        srcType,
			URL:         url,
			Tags:        tags,
			Description: description,
			WatchMode:   watchMode,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := saveSource(cfg, src); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save error: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("added source: %s (id=%s)\nRun source_check to fetch initial state.", src.Name, src.ID)), nil
	}
}

// ---- source_list ----

func sourceListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		typeFilter := req.GetString("type", "")
		tagFilter := req.GetString("tag", "")

		sources, err := allSources(cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("load error: %v", err)), nil
		}
		if len(sources) == 0 {
			return mcp.NewToolResultText("(no sources — use source_add to add one)"), nil
		}

		var lines []string
		for _, src := range sources {
			if typeFilter != "" && src.Type != typeFilter {
				continue
			}
			if tagFilter != "" && !hasTag(src, tagFilter) {
				continue
			}
			tags := ""
			if len(src.Tags) > 0 {
				tags = "  [" + strings.Join(src.Tags, ", ") + "]"
			}
			seen := readSeen(cfg, src.ID)
			seenStr := ""
			if seen != "" {
				seenStr = fmt.Sprintf("  last-seen: %s", truncate(seen, 60))
			}
			desc := ""
			if src.Description != "" {
				desc = "\n    " + src.Description
			}
			lines = append(lines, fmt.Sprintf(
				"[%s] %s (%s)%s\n  id=%s  url=%s%s%s",
				src.Type, src.Name, src.WatchMode, tags, src.ID, src.URL, seenStr, desc,
			))
		}

		if len(lines) == 0 {
			return mcp.NewToolResultText("(no sources match the filter)"), nil
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n\n")), nil
	}
}

func hasTag(src Source, tag string) bool {
	for _, t := range src.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// ---- source_edit ----

func sourceEditHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		src, err := loadSource(cfg, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if v := req.GetString("name", ""); v != "" {
			src.Name = v
		}
		if v := req.GetString("url", ""); v != "" {
			src.URL = v
		}
		if v := req.GetString("tags", ""); v != "" {
			src.Tags = parseTags(v)
		}
		if v := req.GetString("description", ""); v != "" {
			src.Description = v
		}
		if v := req.GetString("watch_mode", ""); v != "" {
			src.WatchMode = v
		}
		src.UpdatedAt = time.Now()

		if err := saveSource(cfg, src); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save error: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("updated source %s", id)), nil
	}
}

// ---- source_remove ----

func sourceRemoveHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path := filepath.Join(sourcesDir(cfg), id+".json")
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				return mcp.NewToolResultError(fmt.Sprintf("source %q not found", id)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("remove error: %v", err)), nil
		}
		// Remove seen marker too
		os.Remove(filepath.Join(seenDir(cfg), id))
		return mcp.NewToolResultText(fmt.Sprintf("removed source %s", id)), nil
	}
}

// ---- source_check ----

func sourceCheckHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		force := req.GetBool("force", false)
		specificID := req.GetString("id", "")

		var sources []Source
		if specificID != "" {
			src, err := loadSource(cfg, specificID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			sources = []Source{src}
		} else {
			var err error
			sources, err = allSources(cfg)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("load error: %v", err)), nil
			}
		}

		if len(sources) == 0 {
			return mcp.NewToolResultText("(no sources — use source_add to add one)"), nil
		}

		var results []CheckResult
		for _, src := range sources {
			result := checkSource(cfg, src, force)
			results = append(results, result)
			if result.HasUpdates {
				EmitEvent(cfg, Event{
					Type: "source_changed",
					ID:   result.SourceID,
					Data: map[string]any{
						"source_id":   result.SourceID,
						"source_name": result.SourceName,
						"source_type": result.Type,
						"summary":     result.Summary,
						"items":       result.Items,
					},
				})
			}
		}

		return mcp.NewToolResultText(formatCheckResults(results)), nil
	}
}

func checkSource(cfg *config.Config, src Source, force bool) CheckResult {
	result := CheckResult{
		SourceID:   src.ID,
		SourceName: src.Name,
		Type:       src.Type,
		CheckedAt:  time.Now().Format(time.RFC3339),
	}

	switch src.Type {
	case "github_repo":
		checkGitHubRepo(cfg, src, force, &result)
	case "github_user":
		checkGitHubUser(cfg, src, force, &result)
	case "rss":
		checkRSS(cfg, src, force, &result)
	case "url":
		checkURL(cfg, src, force, &result)
	case "npm":
		checkNPM(cfg, src, force, &result)
	case "pypi":
		checkPyPI(cfg, src, force, &result)
	default:
		result.Error = fmt.Sprintf("unknown type: %s", src.Type)
	}
	return result
}

// ---- github_repo ----

func checkGitHubRepo(cfg *config.Config, src Source, force bool, r *CheckResult) {
	// Determine what to watch
	mode := src.WatchMode
	if mode == "" {
		mode = "commits"
	}

	var items []string
	var seenKey string

	switch mode {
	case "releases", "all":
		data, err := ghAPIGet(fmt.Sprintf("repos/%s/releases?per_page=5", src.URL))
		if err == nil {
			var releases []map[string]any
			if json.Unmarshal(data, &releases) == nil {
				for _, rel := range releases {
					tag, _ := rel["tag_name"].(string)
					name, _ := rel["name"].(string)
					pub, _ := rel["published_at"].(string)
					items = append(items, fmt.Sprintf("release %s — %s (%s)", tag, name, pub))
				}
				if len(releases) > 0 {
					if tag, ok := releases[0]["tag_name"].(string); ok {
						seenKey = "release:" + tag
					}
				}
			}
		}
		if mode == "releases" {
			break
		}
		fallthrough
	case "commits":
		data, err := ghAPIGet(fmt.Sprintf("repos/%s/commits?per_page=5", src.URL))
		if err == nil {
			var commits []map[string]any
			if json.Unmarshal(data, &commits) == nil {
				for _, c := range commits {
					sha, _ := c["sha"].(string)
					if len(sha) > 8 {
						sha = sha[:8]
					}
					msg := ""
					if commit, ok := c["commit"].(map[string]any); ok {
						msg, _ = commit["message"].(string)
						if idx := strings.Index(msg, "\n"); idx != -1 {
							msg = msg[:idx]
						}
					}
					items = append(items, fmt.Sprintf("%s %s", sha, msg))
				}
				if len(commits) > 0 {
					if sha, ok := commits[0]["sha"].(string); ok {
						seenKey = "commit:" + sha
					}
				}
			}
		}
	case "issues":
		data, err := ghAPIGet(fmt.Sprintf("repos/%s/issues?per_page=5&state=open", src.URL))
		if err == nil {
			var issues []map[string]any
			if json.Unmarshal(data, &issues) == nil {
				for _, issue := range issues {
					num := issue["number"]
					title, _ := issue["title"].(string)
					state, _ := issue["state"].(string)
					items = append(items, fmt.Sprintf("#%v %s [%s]", num, title, state))
				}
				if len(issues) > 0 {
					if num, ok := issues[0]["number"].(float64); ok {
						seenKey = fmt.Sprintf("issue:%d", int(num))
					}
				}
			}
		}
	}

	prevSeen := readSeen(cfg, src.ID)
	if seenKey != "" {
		r.HasUpdates = force || prevSeen != seenKey
		writeSeen(cfg, src.ID, seenKey)
	}
	r.Items = items
	if len(items) > 0 {
		r.Summary = fmt.Sprintf("%d item(s) from %s [%s]", len(items), src.URL, mode)
	} else {
		r.Summary = fmt.Sprintf("no items fetched from %s", src.URL)
	}
}

// ---- github_user ----

func checkGitHubUser(cfg *config.Config, src Source, force bool, r *CheckResult) {
	data, err := ghAPIGet(fmt.Sprintf("users/%s/events/public?per_page=10", src.URL))
	if err != nil {
		r.Error = err.Error()
		return
	}
	var events []map[string]any
	if err := json.Unmarshal(data, &events); err != nil {
		r.Error = err.Error()
		return
	}

	var items []string
	var firstID string
	for _, ev := range events {
		evType, _ := ev["type"].(string)
		repo := ""
		if repoObj, ok := ev["repo"].(map[string]any); ok {
			repo, _ = repoObj["name"].(string)
		}
		createdAt, _ := ev["created_at"].(string)
		id, _ := ev["id"].(string)
		if firstID == "" {
			firstID = id
		}
		items = append(items, fmt.Sprintf("%s on %s (%s)", evType, repo, createdAt))
	}

	prevSeen := readSeen(cfg, src.ID)
	if firstID != "" {
		r.HasUpdates = force || prevSeen != firstID
		writeSeen(cfg, src.ID, firstID)
	}
	r.Items = items
	r.Summary = fmt.Sprintf("%d recent events for @%s", len(items), src.URL)
}

// ---- rss ----

type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string `xml:"title"`
	Updated string `xml:"updated"`
	ID      string `xml:"id"`
	Link    struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
	GUID    string `xml:"guid"`
}

func checkRSS(cfg *config.Config, src Source, force bool, r *CheckResult) {
	body, err := httpGet(src.URL)
	if err != nil {
		r.Error = err.Error()
		return
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 512*1024))
	if err != nil {
		r.Error = err.Error()
		return
	}

	var items []string
	var firstGUID string

	// Try RSS 2.0 first
	var rss rssRoot
	if xml.Unmarshal(data, &rss) == nil && len(rss.Channel.Items) > 0 {
		for i, item := range rss.Channel.Items {
			if i >= 10 {
				break
			}
			guid := item.GUID
			if guid == "" {
				guid = item.Link
			}
			if i == 0 {
				firstGUID = guid
			}
			items = append(items, fmt.Sprintf("%s (%s)", item.Title, item.PubDate))
		}
	} else {
		// Try Atom
		var atom atomFeed
		if xml.Unmarshal(data, &atom) == nil && len(atom.Entries) > 0 {
			for i, entry := range atom.Entries {
				if i >= 10 {
					break
				}
				if i == 0 {
					firstGUID = entry.ID
				}
				items = append(items, fmt.Sprintf("%s (%s)", entry.Title, entry.Updated))
			}
		}
	}

	prevSeen := readSeen(cfg, src.ID)
	if firstGUID != "" {
		r.HasUpdates = force || prevSeen != firstGUID
		writeSeen(cfg, src.ID, firstGUID)
	}
	r.Items = items
	r.Summary = fmt.Sprintf("%d items from feed: %s", len(items), src.Name)
}

// ---- url (change detection by content hash) ----

func checkURL(cfg *config.Config, src Source, force bool, r *CheckResult) {
	body, err := httpGet(src.URL)
	if err != nil {
		r.Error = err.Error()
		return
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1024*1024))
	if err != nil {
		r.Error = err.Error()
		return
	}

	h := sha256.Sum256(data)
	hash := fmt.Sprintf("%x", h[:8])

	prevSeen := readSeen(cfg, src.ID)
	r.HasUpdates = force || prevSeen != hash
	writeSeen(cfg, src.ID, hash)

	if r.HasUpdates && prevSeen != "" {
		r.Items = []string{fmt.Sprintf("content changed (hash %s → %s)", truncate(prevSeen, 8), truncate(hash, 8))}
		r.Summary = fmt.Sprintf("content changed at %s", src.URL)
	} else if prevSeen == "" {
		r.Summary = fmt.Sprintf("initial snapshot taken for %s", src.URL)
	} else {
		r.Summary = fmt.Sprintf("no change detected at %s", src.URL)
	}
}

// ---- npm ----

func checkNPM(cfg *config.Config, src Source, force bool, r *CheckResult) {
	body, err := httpGet(fmt.Sprintf("https://registry.npmjs.org/%s/latest", src.URL))
	if err != nil {
		r.Error = err.Error()
		return
	}
	defer body.Close()
	data, _ := io.ReadAll(body)

	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		r.Error = err.Error()
		return
	}
	version, _ := pkg["version"].(string)
	desc, _ := pkg["description"].(string)

	prevSeen := readSeen(cfg, src.ID)
	r.HasUpdates = force || prevSeen != version
	if version != "" {
		writeSeen(cfg, src.ID, version)
	}
	r.Items = []string{fmt.Sprintf("v%s — %s", version, desc)}
	if r.HasUpdates && prevSeen != "" {
		r.Summary = fmt.Sprintf("npm %s: new version %s (was %s)", src.URL, version, prevSeen)
	} else {
		r.Summary = fmt.Sprintf("npm %s: current version %s", src.URL, version)
	}
}

// ---- pypi ----

func checkPyPI(cfg *config.Config, src Source, force bool, r *CheckResult) {
	body, err := httpGet(fmt.Sprintf("https://pypi.org/pypi/%s/json", src.URL))
	if err != nil {
		r.Error = err.Error()
		return
	}
	defer body.Close()
	data, _ := io.ReadAll(body)

	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		r.Error = err.Error()
		return
	}
	info, _ := pkg["info"].(map[string]any)
	version, _ := info["version"].(string)
	summary, _ := info["summary"].(string)

	prevSeen := readSeen(cfg, src.ID)
	r.HasUpdates = force || prevSeen != version
	if version != "" {
		writeSeen(cfg, src.ID, version)
	}
	r.Items = []string{fmt.Sprintf("v%s — %s", version, summary)}
	if r.HasUpdates && prevSeen != "" {
		r.Summary = fmt.Sprintf("pypi %s: new version %s (was %s)", src.URL, version, prevSeen)
	} else {
		r.Summary = fmt.Sprintf("pypi %s: current version %s", src.URL, version)
	}
}

// ---- source_digest ----

func sourceDigestHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sources, err := allSources(cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("load error: %v", err)), nil
		}
		if len(sources) == 0 {
			return mcp.NewToolResultText("(no sources to digest)"), nil
		}

		var results []CheckResult
		for _, src := range sources {
			r := checkSource(cfg, src, false)
			results = append(results, r)
			if r.HasUpdates {
				EmitEvent(cfg, Event{
					Type: "source_changed",
					ID:   r.SourceID,
					Data: map[string]any{
						"source_id":   r.SourceID,
						"source_name": r.SourceName,
						"source_type": r.Type,
						"summary":     r.Summary,
						"items":       r.Items,
					},
				})
			}
		}

		text := formatCheckResults(results)

		// Post to Slack
		if req.GetBool("post_slack", false) {
			ch := req.GetString("slack_channel", "")
			if ch != "" && cfg.SlackToken != "" {
				slackAPICall(cfg, "POST", "chat.postMessage", map[string]any{
					"channel": ch,
					"text":    "```\n" + text + "\n```",
				})
			}
		}

		// Post to Discord
		if req.GetBool("post_discord", false) {
			chID := req.GetString("discord_channel_id", "")
			if chID != "" && cfg.DiscordToken != "" {
				discordDo(cfg, "POST", "/channels/"+chID+"/messages",
					map[string]string{"content": "```\n" + text + "\n```"})
			}
		}

		return mcp.NewToolResultText(text), nil
	}
}

// ---- formatting ----

func formatCheckResults(results []CheckResult) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("Source Digest — %s", time.Now().Format("2006-01-02 15:04")))

	updated := 0
	for _, r := range results {
		if r.HasUpdates {
			updated++
		}
	}
	lines = append(lines, fmt.Sprintf("%d/%d sources have updates\n", updated, len(results)))

	for _, r := range results {
		icon := "·"
		if r.HasUpdates {
			icon = "★"
		}
		if r.Error != "" {
			icon = "✗"
		}

		lines = append(lines, fmt.Sprintf("%s [%s] %s", icon, r.Type, r.SourceName))
		if r.Error != "" {
			lines = append(lines, "  error: "+r.Error)
			continue
		}
		lines = append(lines, "  "+r.Summary)
		for i, item := range r.Items {
			if i >= 5 {
				lines = append(lines, fmt.Sprintf("  … and %d more", len(r.Items)-5))
				break
			}
			lines = append(lines, "  - "+item)
		}
	}
	return strings.Join(lines, "\n")
}

// ---- HTTP helpers ----

func httpGet(url string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fafb/2.0")
	req.Header.Set("Accept", "application/json, application/xml, text/xml, */*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}

func ghAPIGet(path string) ([]byte, error) {
	body, err := httpGet("https://api.github.com/" + path)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

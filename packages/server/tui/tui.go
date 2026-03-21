// Package tui provides a split-pane terminal UI for fafb.
//
// Layout (Option A — fixed panes):
//
//	┌─ SOURCES ────────────┬─ Detail / Output ────────────────┐
//	│ ★ mark3labs/mcp-go   │ [selected item content]          │
//	│ · fastapi            │                                   │
//	│ · rust-blog          │                                   │
//	├─ PENDING ────────────┤                                   │
//	│ [pending] refactor…  │                                   │
//	│ [approved] deps…     │                                   │
//	├─ LEARNING ───────────┤                                   │
//	│ python 7/10          │                                   │
//	│ japanese             │                                   │
//	└──────────────────────┴──────────────────────────────────┘
//	 [tab] switch panel  [a]dd  [e]dit  [d]elete  [r]efresh  [?]help  [q]quit
//
// Navigation: Tab cycles panels, arrow keys move within a panel,
// Enter selects (shows detail in right pane), action keys trigger operations.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"

	"github.com/caboose-mcp/server/config"
)

// ---- styles (inherit terminal colors, no hardcoded palette) ----

var (
	stylePanelTitle  = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	stylePanelBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	stylePanelFocus  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12"))
	styleStatusBar   = lipgloss.NewStyle().Padding(0, 1).Faint(true)
	styleHelp        = lipgloss.NewStyle().Faint(true)
	styleHeader      = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	styleNew         = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleWarn        = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	styleErr         = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
)

// ---- panel IDs ----

type panelID int

const (
	panelSources panelID = iota
	panelPending
	panelLearning
	panelCount
)

var panelNames = map[panelID]string{
	panelSources:  "SOURCES",
	panelPending:  "PENDING",
	panelLearning: "LEARNING",
}

// ---- list item types ----

type sourceItem struct {
	id       string
	srcType  string
	name     string
	hasNew   bool
	lastSeen string
}

func (s sourceItem) Title() string {
	icon := "·"
	if s.hasNew {
		icon = styleNew.Render("★")
	}
	return icon + " " + s.name
}
func (s sourceItem) Description() string { return s.srcType + "  " + s.id }
func (s sourceItem) FilterValue() string { return s.name }

type pendingItem struct {
	id       string
	status   string
	title    string
	category string
}

func (p pendingItem) Title() string {
	color := styleWarn
	if p.status == "approved" {
		color = styleNew
	} else if p.status == "rejected" {
		color = styleErr
	}
	return color.Render("["+p.status+"]") + " " + p.title
}
func (p pendingItem) Description() string { return p.category + "  id=" + p.id }
func (p pendingItem) FilterValue() string { return p.title }

type learningItem struct {
	language string
	mode     string
	score    int
	total    int
	lastSeen string
}

func (l learningItem) Title() string {
	pct := 0
	if l.total > 0 {
		pct = (l.score * 100) / l.total
	}
	return fmt.Sprintf("%s  %d/%d (%d%%)", l.language, l.score, l.total, pct)
}
func (l learningItem) Description() string { return l.mode + "  last: " + l.lastSeen }
func (l learningItem) FilterValue() string { return l.language }

// ---- model ----

type model struct {
	cfg         *config.Config
	width       int
	height      int
	activePanel panelID

	lists         [panelCount]list.Model
	detail        viewport.Model
	detailContent string

	statusMsg string
	showHelp  bool
}

// ---- messages ----

type loadedMsg struct{}
type statusMsg string
type detailMsg string
type tickMsg struct{} // file watcher tick to refresh data

// ---- init ----

func newModel(cfg *config.Config) model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true

	var lists [panelCount]list.Model
	for i := panelID(0); i < panelCount; i++ {
		l := list.New(nil, delegate, 0, 0)
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(false)
		l.SetShowHelp(false)
		lists[i] = l
	}

	vp := viewport.New(0, 0)

	return model{
		cfg:    cfg,
		lists:  lists,
		detail: vp,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadAll(), m.watchDirs())
}

// ---- load data from disk ----

func (m model) loadAll() tea.Cmd {
	return func() tea.Msg {
		return loadedMsg{}
	}
}

// watchDirs monitors the state directories for changes and triggers refresh on file events.
func (m model) watchDirs() tea.Cmd {
	return func() tea.Msg {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return statusMsg("Watch error: " + err.Error())
		}
		defer watcher.Close()

		// Watch state directories
		dirs := []string{
			filepath.Join(m.cfg.ClaudeDir, "pending"),
			filepath.Join(m.cfg.ClaudeDir, "sources"),
			filepath.Join(m.cfg.ClaudeDir, "learning"),
		}
		for _, dir := range dirs {
			// Ensure directory exists before watching
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return statusMsg("Watch setup error: " + err.Error())
			}
			if err := watcher.Add(dir); err != nil {
				return statusMsg("Watch error: " + err.Error())
			}
		}

		// Block until an event occurs
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return tickMsg{}
				}
				// Only respond to write, create, or remove events
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) != 0 {
					return tickMsg{}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return tickMsg{}
				}
				return statusMsg("Watch error: " + err.Error())
			}
		}
	}
}

func (m *model) refreshData() {
	m.refreshSources()
	m.refreshPending()
	m.refreshLearning()
}

func (m *model) refreshSources() {
	sourcesDir := filepath.Join(m.cfg.ClaudeDir, "sources")
	seenDir := filepath.Join(sourcesDir, ".seen")
	entries, err := os.ReadDir(sourcesDir)
	if err != nil {
		return
	}

	var items []list.Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourcesDir, e.Name()))
		if err != nil {
			continue
		}
		// Quick JSON parse for name/type/id
		var m2 map[string]any
		if parseSimpleJSON(data, &m2) != nil {
			continue
		}
		id, _ := m2["id"].(string)
		name, _ := m2["name"].(string)
		srcType, _ := m2["type"].(string)
		// Check if seen marker exists
		seen := ""
		if seenData, err := os.ReadFile(filepath.Join(seenDir, id)); err == nil {
			seen = strings.TrimSpace(string(seenData))
		}
		items = append(items, sourceItem{
			id: id, srcType: srcType, name: name, lastSeen: seen,
		})
	}
	m.lists[panelSources].SetItems(items)
}

func (m *model) refreshPending() {
	pendingDir := filepath.Join(m.cfg.ClaudeDir, "pending")
	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		return
	}

	var items []list.Item
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pendingDir, e.Name()))
		if err != nil {
			continue
		}
		var m2 map[string]any
		if parseSimpleJSON(data, &m2) != nil {
			continue
		}
		id, _ := m2["id"].(string)
		status, _ := m2["status"].(string)
		title, _ := m2["title"].(string)
		category, _ := m2["category"].(string)
		if status == "pending" || status == "approved" {
			items = append(items, pendingItem{id: id, status: status, title: title, category: category})
		}
	}
	m.lists[panelPending].SetItems(items)
}

func (m *model) refreshLearning() {
	baseDir := filepath.Join(m.cfg.ClaudeDir, "learning")
	langDirs, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}

	var items []list.Item
	for _, d := range langDirs {
		if !d.IsDir() {
			continue
		}
		sessions, _ := os.ReadDir(filepath.Join(baseDir, d.Name()))
		for _, sf := range sessions {
			if !strings.HasSuffix(sf.Name(), ".json") {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(baseDir, d.Name(), sf.Name()))
			var m2 map[string]any
			if parseSimpleJSON(data, &m2) != nil {
				continue
			}
			lang, _ := m2["language"].(string)
			mode, _ := m2["mode"].(string)
			score, _ := m2["score"].(float64)
			total, _ := m2["total_asked"].(float64)
			last, _ := m2["last_active"].(string)
			if len(last) > 10 {
				last = last[:10]
			}
			items = append(items, learningItem{
				language: lang, mode: mode,
				score: int(score), total: int(total), lastSeen: last,
			})
		}
	}
	m.lists[panelLearning].SetItems(items)
}

// ---- update ----

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePanels()
		m.refreshData()
		return m, nil

	case loadedMsg:
		m.refreshData()
		return m, nil

	case tickMsg:
		m.refreshData()
		return m, m.watchDirs() // re-arm the watcher

	case statusMsg:
		m.statusMsg = string(msg)
		return m, nil

	case detailMsg:
		m.detailContent = string(msg)
		m.detail.SetContent(string(msg))
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Delegate to active list or viewport
	var cmd tea.Cmd
	if m.activePanel < panelCount {
		m.lists[m.activePanel], cmd = m.lists[m.activePanel].Update(msg)
	} else {
		m.detail, cmd = m.detail.Update(msg)
	}
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.activePanel = (m.activePanel + 1) % panelCount
		return m, nil

	case "shift+tab":
		m.activePanel = (m.activePanel + panelCount - 1) % panelCount
		return m, nil

	case "?":
		m.showHelp = !m.showHelp
		return m, nil

	case "r":
		m.statusMsg = "Refreshing…"
		m.refreshData()
		return m, func() tea.Msg { return statusMsg("Refreshed") }

	case "enter":
		return m.selectCurrent()

	case "a":
		return m.handleAdd()

	case "d":
		return m.handleDelete()

	case "c":
		if m.activePanel == panelSources {
			return m.handleCheck()
		}

	case "A":
		// Approve pending suggestion
		if m.activePanel == panelPending {
			return m.handleApprove()
		}
	}

	// Forward to active panel
	var cmd tea.Cmd
	m.lists[m.activePanel], cmd = m.lists[m.activePanel].Update(msg)
	return m, cmd
}

func (m model) selectCurrent() (model, tea.Cmd) {
	if m.activePanel >= panelCount {
		return m, nil
	}
	sel := m.lists[m.activePanel].SelectedItem()
	if sel == nil {
		return m, nil
	}

	var content string
	switch item := sel.(type) {
	case sourceItem:
		data, _ := os.ReadFile(filepath.Join(m.cfg.ClaudeDir, "sources", item.id+".json"))
		content = string(data)
	case pendingItem:
		data, _ := os.ReadFile(filepath.Join(m.cfg.ClaudeDir, "pending", item.id+".json"))
		content = string(data)
	case learningItem:
		content = fmt.Sprintf("Language: %s\nMode: %s\nScore: %d/%d", item.language, item.mode, item.score, item.total)
	}

	m.detailContent = content
	m.detail.SetContent(content)
	return m, nil
}

func (m model) handleCheck() (model, tea.Cmd) {
	sel := m.lists[panelSources].SelectedItem()
	if sel == nil {
		return m, nil
	}
	item, ok := sel.(sourceItem)
	if !ok {
		return m, nil
	}
	return m, func() tea.Msg {
		out, err := runMCPTool("source_check", map[string]any{"id": item.id, "force": false})
		if err != nil {
			return detailMsg("Error: " + err.Error())
		}
		return detailMsg(out)
	}
}

func (m model) handleApprove() (model, tea.Cmd) {
	sel := m.lists[panelPending].SelectedItem()
	if sel == nil {
		return m, nil
	}
	item, ok := sel.(pendingItem)
	if !ok {
		return m, nil
	}
	return m, func() tea.Msg {
		out, err := runMCPTool("si_approve", map[string]any{"id": item.id})
		if err != nil {
			return statusMsg("Approve error: " + err.Error())
		}
		return statusMsg(out)
	}
}

func (m model) handleAdd() (model, tea.Cmd) {
	hint := map[panelID]string{
		panelSources:  "Use Claude: source_add type=github_repo url=owner/repo",
		panelPending:  "Use Claude: si_suggest title=... description=... category=...",
		panelLearning: "Use Claude: learn_start language=python",
	}
	m.statusMsg = hint[m.activePanel]
	return m, nil
}

func (m model) handleDelete() (model, tea.Cmd) {
	if m.activePanel != panelSources {
		return m, nil
	}
	sel := m.lists[panelSources].SelectedItem()
	if sel == nil {
		return m, nil
	}
	item, ok := sel.(sourceItem)
	if !ok {
		return m, nil
	}
	return m, func() tea.Msg {
		out, err := runMCPTool("source_remove", map[string]any{"id": item.id})
		if err != nil {
			return statusMsg("Remove error: " + err.Error())
		}
		return statusMsg(out)
	}
}

// ---- layout ----

func (m *model) resizePanels() {
	if m.width == 0 || m.height == 0 {
		return
	}
	statusH := 2
	contentH := m.height - statusH - 2 // borders

	leftW := m.width / 3
	rightW := m.width - leftW - 3 // borders

	panelH := contentH / int(panelCount)

	for i := panelID(0); i < panelCount; i++ {
		m.lists[i].SetSize(leftW-2, panelH-2)
	}
	m.detail.Width = rightW
	m.detail.Height = contentH - 2
}

// ---- view ----

func (m model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	if m.showHelp {
		return m.helpView()
	}

	statusH := 2
	contentH := m.height - statusH - 2
	leftW := m.width / 3
	rightW := m.width - leftW - 3
	panelH := contentH / int(panelCount)

	// Left column: stacked panels
	var leftPanels []string
	for i := panelID(0); i < panelCount; i++ {
		title := stylePanelTitle.Render(panelNames[i])
		content := m.lists[i].View()
		inner := lipgloss.JoinVertical(lipgloss.Left, title, content)

		style := stylePanelBorder
		if m.activePanel == i {
			style = stylePanelFocus
		}
		panel := style.Width(leftW - 2).Height(panelH - 2).Render(inner)
		leftPanels = append(leftPanels, panel)
	}
	left := lipgloss.JoinVertical(lipgloss.Left, leftPanels...)

	// Right column: detail viewport
	detailTitle := styleHeader.Render("Detail")
	detailStyle := stylePanelBorder.Width(rightW - 2).Height(contentH - 2)
	right := detailStyle.Render(lipgloss.JoinVertical(lipgloss.Left, detailTitle, m.detail.View()))

	// Join columns
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	// Status bar
	keyHints := "[tab] switch  [↑↓] navigate  [enter] detail  [r] refresh  [c] check  [A] approve  [d] delete  [?] help  [q] quit"
	status := m.statusMsg
	statusBar := styleStatusBar.Width(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Left,
			styleHelp.Render(keyHints),
			strings.Repeat(" ", max(0, m.width-len(keyHints)-len(status)-2)),
			status,
		),
	)

	return lipgloss.JoinVertical(lipgloss.Left, body, statusBar)
}

func (m model) helpView() string {
	help := `fafb TUI — keyboard shortcuts

Navigation
  tab / shift+tab   cycle between panels (Sources, Pending, Learning)
  ↑ / ↓             move within panel
  enter             show detail in right pane

Sources panel
  c                 check selected source for updates
  d                 remove selected source
  a                 show hint for adding a source via Claude

Pending panel
  A                 approve selected suggestion
  a                 show hint for creating a suggestion via Claude

Global
  r                 refresh all panels from disk
  ?                 toggle this help
  q / ctrl+c        quit

Adding items
  All write operations (source_add, si_suggest, learn_start, etc.) are
  done through the MCP tools in Claude. The TUI is read-focused with
  quick actions for approve/check/delete.
`
	return stylePanelBorder.Width(m.width - 2).Height(m.height - 4).Render(help)
}

// ---- MCP tool caller (subprocess) ----

func runMCPTool(toolName string, args map[string]any) (string, error) {
	// Find own binary path
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}

	import_json_marshal_call := func(v any) []byte {
		b, _ := jsonMarshalSimple(v)
		return b
	}

	input := import_json_marshal_call(payload)

	cmd := exec.Command(exe)
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Parse response
	var resp map[string]any
	if parseSimpleJSON(out, &resp) != nil {
		return string(out), nil
	}
	result, _ := resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) > 0 {
		first, _ := content[0].(map[string]any)
		text, _ := first["text"].(string)
		return text, nil
	}
	return string(out), nil
}

// ---- helpers ----

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// parseSimpleJSON delegates to encoding/json (available in binary via other packages).
func parseSimpleJSON(data []byte, v any) error {
	return jsonUnmarshalSimple(data, v)
}

// Run starts the TUI. Called from main when --tui flag is set.
func Run(cfg *config.Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

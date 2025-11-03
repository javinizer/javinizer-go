package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// Component stubs - these provide basic functionality
// Can be enhanced later with full Bubbles components

// Header component
type Header struct {
	width int
	stats worker.ProgressStats
}

func NewHeader() *Header {
	return &Header{}
}

func (h *Header) SetWidth(width int) {
	h.width = width
}

func (h *Header) UpdateStats(stats worker.ProgressStats) {
	h.stats = stats
}

func (h *Header) View() string {
	// Simple header - will be enhanced
	return HeaderStyle.Render("Javinizer TUI")
}

// Browser component
type Browser struct {
	items       []FileItem
	cursor      int
	width       int
	height      int
	selected    map[string]bool
	sourcePath  string // Current scan path
	destPath    string // Destination path for organized files
	pathDisplay string // Formatted path display for the view
}

func NewBrowser() *Browser {
	return &Browser{
		items:    make([]FileItem, 0),
		selected: make(map[string]bool),
	}
}

func (b *Browser) SetSize(width, height int) {
	b.width = width
	b.height = height
}

func (b *Browser) SetItems(items []FileItem) {
	b.items = items
}

func (b *Browser) SetSourcePath(path string) {
	b.sourcePath = path
	b.pathDisplay = path
}

func (b *Browser) SetDestPath(path string) {
	b.destPath = path
}

func (b *Browser) SetPathDisplay(display string) {
	b.pathDisplay = display
}

func (b *Browser) CursorUp() {
	if b.cursor > 0 {
		b.cursor--
	}
}

func (b *Browser) CursorDown() {
	if b.cursor < len(b.items)-1 {
		b.cursor++
	}
}

func (b *Browser) ToggleSelection(path string) {
	// Find the item
	var targetItem *FileItem
	for i := range b.items {
		if b.items[i].Path == path {
			targetItem = &b.items[i]
			break
		}
	}

	if targetItem == nil {
		return
	}

	// If it's a directory, toggle all files within it
	if targetItem.IsDir {
		isCurrentlySelected := b.selected[path]
		newState := !isCurrentlySelected

		// Toggle all files in this directory
		for i := range b.items {
			if !b.items[i].IsDir && filepath.Dir(b.items[i].Path) == path {
				b.selected[b.items[i].Path] = newState
			}
		}

		// Toggle the folder marker itself
		b.selected[path] = newState
	} else {
		// Regular file toggle
		b.selected[path] = !b.selected[path]
	}
}

func (b *Browser) SelectAll() {
	for _, item := range b.items {
		b.selected[item.Path] = true
	}
}

func (b *Browser) DeselectAll() {
	b.selected = make(map[string]bool)
}

func (b *Browser) Init() tea.Cmd {
	return nil
}

func (b *Browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return b, nil
}

func (b *Browser) View() string {
	// Title
	view := Title("Files") + " " + Dimmed("(f:source o:output)") + "\n"

	// Source path
	sourceDisplay := b.sourcePath
	if sourceDisplay == "" {
		sourceDisplay = "."
	}
	if len(sourceDisplay) > 40 {
		sourceDisplay = "..." + sourceDisplay[len(sourceDisplay)-37:]
	}
	view += Dimmed("From: ") + Highlight(sourceDisplay) + "\n"

	// Destination path
	destDisplay := b.destPath
	if destDisplay == "" {
		destDisplay = sourceDisplay // Default to source
	}
	if len(destDisplay) > 40 {
		destDisplay = "..." + destDisplay[len(destDisplay)-37:]
	}
	view += Dimmed("To:   ") + Highlight(destDisplay) + "\n\n"

	if len(b.items) == 0 {
		return view + Dimmed("No files found")
	}

	// Show items around cursor
	start := b.cursor - 5
	if start < 0 {
		start = 0
	}
	end := start + b.height - 4
	if end > len(b.items) {
		end = len(b.items)
	}

	for i := start; i < end; i++ {
		item := b.items[i]
		cursor := "  "
		if i == b.cursor {
			cursor = "> "
		}

		// Tree indentation based on depth
		indent := strings.Repeat("  ", item.Depth)

		// Determine checkbox state
		checkbox := "☐ "
		if item.IsDir {
			// For folders, check if all children are selected
			allChildrenSelected := true
			hasChildren := false
			for j := range b.items {
				if !b.items[j].IsDir && filepath.Dir(b.items[j].Path) == item.Path {
					hasChildren = true
					if !b.selected[b.items[j].Path] {
						allChildrenSelected = false
						break
					}
				}
			}
			if hasChildren && allChildrenSelected {
				checkbox = Success("☑ ")
			}
		} else {
			// For files, check direct selection
			if b.selected[item.Path] {
				checkbox = Success("☑ ")
			}
		}

		// Add folder icon for directories
		icon := ""
		if item.IsDir {
			icon = "📁 "
		}

		name := item.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		// Show matched status for files
		matchIndicator := ""
		if !item.IsDir && item.Matched {
			matchIndicator = " " + Dimmed("["+item.ID+"]")
		}

		view += cursor + indent + checkbox + icon + name + matchIndicator + "\n"
	}

	view += fmt.Sprintf("\n%d/%d files", b.cursor+1, len(b.items))
	return view
}

// TaskList component
type TaskList struct {
	tasks  map[string]*worker.TaskProgress
	order  []string
	width  int
	height int
}

func NewTaskList() *TaskList {
	return &TaskList{
		tasks: make(map[string]*worker.TaskProgress),
		order: make([]string, 0),
	}
}

func (t *TaskList) SetSize(width, height int) {
	t.width = width
	t.height = height
}

func (t *TaskList) UpdateTask(update worker.ProgressUpdate) {
	if _, exists := t.tasks[update.TaskID]; !exists {
		t.order = append(t.order, update.TaskID)
	}

	t.tasks[update.TaskID] = &worker.TaskProgress{
		ID:        update.TaskID,
		Type:      update.Type,
		Status:    update.Status,
		Progress:  update.Progress,
		Message:   update.Message,
		BytesDone: update.BytesDone,
		UpdatedAt: update.Timestamp,
		Error:     update.Error,
	}
}

func (t *TaskList) Init() tea.Cmd {
	return nil
}

func (t *TaskList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return t, nil
}

func (t *TaskList) View() string {
	view := Title("Tasks") + "\n\n"

	if len(t.tasks) == 0 {
		return view + Dimmed("No active tasks")
	}

	// Show last N tasks
	start := len(t.order) - (t.height - 4)
	if start < 0 {
		start = 0
	}

	for i := start; i < len(t.order); i++ {
		taskID := t.order[i]
		task := t.tasks[taskID]

		status := ""
		switch task.Status {
		case worker.TaskStatusRunning:
			status = RunningBadge.Render("RUN")
		case worker.TaskStatusSuccess:
			status = SuccessBadge.Render("OK")
		case worker.TaskStatusFailed:
			status = ErrorBadge.Render("ERR")
		case worker.TaskStatusPending:
			status = InfoBadge.Render("...")
		}

		progress := renderProgressBar(task.Progress, 20)
		view += fmt.Sprintf("%s %s %s\n", status, progress, task.ID)
	}

	return view
}

// Dashboard component
type Dashboard struct {
	stats       worker.ProgressStats
	elapsedTime time.Duration
	width       int
	height      int
}

func NewDashboard() *Dashboard {
	return &Dashboard{}
}

func (d *Dashboard) SetSize(width, height int) {
	d.width = width
	d.height = height
}

func (d *Dashboard) UpdateStats(stats worker.ProgressStats, elapsed time.Duration) {
	d.stats = stats
	d.elapsedTime = elapsed
}

func (d *Dashboard) Init() tea.Cmd {
	return nil
}

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return d, nil
}

func (d *Dashboard) View() string {
	view := Title("Dashboard") + "\n\n"

	view += fmt.Sprintf("Total:     %d\n", d.stats.Total)
	view += fmt.Sprintf("Running:   %s\n", RunningBadge.Render(fmt.Sprintf("%d", d.stats.Running)))
	view += fmt.Sprintf("Success:   %s\n", Success(fmt.Sprintf("%d", d.stats.Success)))
	view += fmt.Sprintf("Failed:    %s\n", Error(fmt.Sprintf("%d", d.stats.Failed)))
	view += fmt.Sprintf("\nProgress:  %.1f%%\n", d.stats.OverallProgress*100)
	view += fmt.Sprintf("Elapsed:   %v\n", d.elapsedTime.Round(time.Second))

	return view
}

// LogViewer component
type LogViewer struct {
	logs       []LogEntry
	scroll     int
	autoScroll bool
	width      int
	height     int
}

func NewLogViewer() *LogViewer {
	return &LogViewer{
		logs:       make([]LogEntry, 0),
		autoScroll: true,
	}
}

func (l *LogViewer) SetSize(width, height int) {
	l.width = width
	l.height = height
}

func (l *LogViewer) AddLog(entry LogEntry) {
	l.logs = append(l.logs, entry)
	if l.autoScroll {
		l.scroll = len(l.logs) - 1
	}
}

func (l *LogViewer) ScrollUp() {
	if l.scroll > 0 {
		l.scroll--
	}
}

func (l *LogViewer) ScrollDown() {
	if l.scroll < len(l.logs)-1 {
		l.scroll++
	}
}

func (l *LogViewer) ScrollToTop() {
	l.scroll = 0
}

func (l *LogViewer) ScrollToBottom() {
	l.scroll = len(l.logs) - 1
}

func (l *LogViewer) ToggleAutoScroll() {
	l.autoScroll = !l.autoScroll
}

func (l *LogViewer) Init() tea.Cmd {
	return nil
}

func (l *LogViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return l, nil
}

func (l *LogViewer) View() string {
	view := Title("Logs") + "\n\n"

	if len(l.logs) == 0 {
		return view + Dimmed("No logs yet")
	}

	// Show logs around scroll position
	start := l.scroll - l.height + 4
	if start < 0 {
		start = 0
	}
	end := l.scroll + 1
	if end > len(l.logs) {
		end = len(l.logs)
	}

	for i := start; i < end; i++ {
		log := l.logs[i]
		timestamp := log.Timestamp.Format("15:04:05")

		var levelStyle lipgloss.Style
		switch log.Level {
		case "debug":
			levelStyle = LogDebugStyle
		case "info":
			levelStyle = LogInfoStyle
		case "warn":
			levelStyle = LogWarnStyle
		case "error":
			levelStyle = LogErrorStyle
		default:
			levelStyle = LogInfoStyle
		}

		level := levelStyle.Render(fmt.Sprintf("[%-5s]", log.Level))

		// Wrap long messages to fit width
		// Account for timestamp (8) + level (7) + spacing (2) = 17 chars
		maxMessageWidth := l.width - 17
		if maxMessageWidth < 40 {
			maxMessageWidth = 40
		}

		message := log.Message
		if len(message) > maxMessageWidth {
			// Word wrap the message
			words := strings.Fields(message)
			var lines []string
			currentLine := ""

			for _, word := range words {
				if len(currentLine)+len(word)+1 <= maxMessageWidth {
					if currentLine == "" {
						currentLine = word
					} else {
						currentLine += " " + word
					}
				} else {
					if currentLine != "" {
						lines = append(lines, currentLine)
					}
					currentLine = word
				}
			}
			if currentLine != "" {
				lines = append(lines, currentLine)
			}

			// Render first line with timestamp and level
			if len(lines) > 0 {
				view += fmt.Sprintf("%s %s %s\n", Dimmed(timestamp), level, lines[0])
				// Continuation lines with indentation
				for j := 1; j < len(lines); j++ {
					view += fmt.Sprintf("%s %s %s\n", strings.Repeat(" ", 8), strings.Repeat(" ", 7), lines[j])
				}
			}
		} else {
			view += fmt.Sprintf("%s %s %s\n", Dimmed(timestamp), level, message)
		}
	}

	return view
}

// HelpView component
type HelpView struct {
	width  int
	height int
}

func NewHelpView() *HelpView {
	return &HelpView{}
}

func (h *HelpView) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h *HelpView) Init() tea.Cmd {
	return nil
}

func (h *HelpView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return h, nil
}

func (h *HelpView) View() string {
	help := Title("Help") + "\n\n"

	help += HelpKeyStyle.Render("Global Keys") + "\n"
	help += "  ? - Toggle help\n"
	help += "  q/Ctrl+C - Quit\n"
	help += "  1-3 - Switch views\n"
	help += "  Tab - Cycle views\n\n"

	help += HelpKeyStyle.Render("Browser View") + "\n"
	help += "  m - Manual search (select scrapers + ID/URL)\n"
	help += "  f - Change scan folder\n"
	help += "  r - Refresh/rescan current folder\n"
	help += "  ↑/k - Move up\n"
	help += "  ↓/j - Move down\n"
	help += "  Space - Toggle selection (files or entire folders)\n"
	help += "  a - Select all\n"
	help += "  A - Deselect all\n"
	help += "  Enter - Start processing\n"
	help += "  p - Pause/resume\n\n"

	help += HelpKeyStyle.Render("Logs View") + "\n"
	help += "  ↑/k - Scroll up\n"
	help += "  ↓/j - Scroll down\n"
	help += "  g - Go to top\n"
	help += "  G - Go to bottom\n"
	help += "  a - Toggle auto-scroll\n\n"

	help += Dimmed("📁 Folders with checkboxes select all files inside")

	return help
}

// SettingsView component
type SettingsView struct {
	width               int
	height              int
	cursor              int
	dryRun              bool
	forceUpdate         bool
	forceRefresh        bool
	moveFiles           bool
	scrapeEnabled       bool
	downloadEnabled     bool
	downloadExtrafanart bool
	organizeEnabled     bool
	nfoEnabled          bool
	updateMode          bool
}

func NewSettingsView() *SettingsView {
	return &SettingsView{
		scrapeEnabled:   true,
		downloadEnabled: true,
		organizeEnabled: true,
		nfoEnabled:      true,
	}
}

func (s *SettingsView) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s *SettingsView) Init() tea.Cmd {
	return nil
}

func (s *SettingsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return s, nil
}

func (s *SettingsView) UpdateSettings(
	cursor int,
	dryRun, forceUpdate, forceRefresh, moveFiles bool,
	scrapeEnabled, downloadEnabled, downloadExtrafanart, organizeEnabled, nfoEnabled, updateMode bool,
) {
	s.cursor = cursor
	s.dryRun = dryRun
	s.forceUpdate = forceUpdate
	s.forceRefresh = forceRefresh
	s.moveFiles = moveFiles
	s.scrapeEnabled = scrapeEnabled
	s.downloadEnabled = downloadEnabled
	s.downloadExtrafanart = downloadExtrafanart
	s.organizeEnabled = organizeEnabled
	s.nfoEnabled = nfoEnabled
	s.updateMode = updateMode
}

func (s *SettingsView) View() string {
	view := Title("Settings") + " " + Dimmed("(space to toggle)") + "\n\n"

	settings := []struct {
		index   int
		name    string
		desc    string
		enabled bool
	}{
		{0, "Dry Run", "Preview mode - don't make actual changes", s.dryRun},
		{1, "Force Update", "Replace existing files (images, NFO)", s.forceUpdate},
		{2, "Force Refresh", "Clear DB cache and rescrape metadata", s.forceRefresh},
		{3, "Move Files", "Move instead of copy (default: copy)", s.moveFiles},
		{4, "Scrape Metadata", "Fetch metadata from JAV sources", s.scrapeEnabled},
		{5, "Download Media", "Download covers, screenshots, trailers", s.downloadEnabled},
		{6, "Download Extrafanart", "Download extrafanart/screenshots to subfolder", s.downloadExtrafanart},
		{7, "Organize Files", "Move/copy files to organized structure", s.organizeEnabled},
		{8, "Generate NFO", "Create NFO files for media centers", s.nfoEnabled},
		{9, "Update Mode", "Only create/update metadata, don't move files", s.updateMode},
	}

	for _, setting := range settings {
		cursorStr := "  "
		if s.cursor == setting.index {
			cursorStr = "> "
		}

		checkbox := "☐"
		if setting.enabled {
			checkbox = Success("☑")
		}

		view += fmt.Sprintf("%s%s %s\n", cursorStr, checkbox, HelpKeyStyle.Render(setting.name))
		view += fmt.Sprintf("   %s\n\n", Dimmed(setting.desc))
	}

	view += "\n" + Dimmed("Changes take effect on next processing run")

	return view
}

// Console component - shows live output and metadata preview
type Console struct {
	width      int
	height     int
	entries    []string
	maxEntries int
	autoScroll bool
	scroll     int
}

func NewConsole() *Console {
	return &Console{
		entries:    make([]string, 0),
		maxEntries: 1000,
		autoScroll: true,
		scroll:     0,
	}
}

func (c *Console) SetSize(width, height int) {
	c.width = width
	c.height = height
}

func (c *Console) Init() tea.Cmd {
	return nil
}

func (c *Console) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return c, nil
}

func (c *Console) AddEntry(entry string) {
	c.entries = append(c.entries, entry)

	// Trim if exceeds max
	if len(c.entries) > c.maxEntries {
		c.entries = c.entries[len(c.entries)-c.maxEntries:]
	}

	// Auto-scroll to bottom
	if c.autoScroll {
		c.ScrollToBottom()
	}
}

func (c *Console) Clear() {
	c.entries = make([]string, 0)
	c.scroll = 0
}

func (c *Console) ScrollUp() {
	if c.scroll > 0 {
		c.scroll--
	}
}

func (c *Console) ScrollDown() {
	maxScroll := len(c.entries) - c.height + 3
	if maxScroll < 0 {
		maxScroll = 0
	}
	if c.scroll < maxScroll {
		c.scroll++
	}
}

func (c *Console) ScrollToTop() {
	c.scroll = 0
}

func (c *Console) ScrollToBottom() {
	maxScroll := len(c.entries) - c.height + 3
	if maxScroll < 0 {
		maxScroll = 0
	}
	c.scroll = maxScroll
}

func (c *Console) ToggleAutoScroll() {
	c.autoScroll = !c.autoScroll
}

func (c *Console) View() string {
	view := Title("Console") + "\n"

	if len(c.entries) == 0 {
		return view + Dimmed("No output yet...")
	}

	// Calculate visible range
	visibleHeight := c.height - 2 // Account for title
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	start := c.scroll
	if start < 0 {
		start = 0
	}
	end := start + visibleHeight
	if end > len(c.entries) {
		end = len(c.entries)
		start = end - visibleHeight
		if start < 0 {
			start = 0
		}
	}

	// Render entries
	for i := start; i < end; i++ {
		entry := c.entries[i]

		// Word wrap if needed
		maxWidth := c.width - 2
		if maxWidth < 40 {
			maxWidth = 40
		}

		if len(entry) > maxWidth {
			// Simple wrapping - split into chunks
			for len(entry) > 0 {
				if len(entry) > maxWidth {
					view += entry[:maxWidth] + "\n"
					entry = entry[maxWidth:]
				} else {
					view += entry + "\n"
					break
				}
			}
		} else {
			view += entry + "\n"
		}
	}

	// Show scroll indicator if not all entries visible
	if len(c.entries) > visibleHeight {
		view += Dimmed(fmt.Sprintf("[%d/%d]", end, len(c.entries)))
	}

	return view
}

// Helper functions

func renderProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := ProgressBarStyle.Render(strings.Repeat("█", filled))
	bar += ProgressEmptyStyle.Render(strings.Repeat("░", empty))

	return fmt.Sprintf("[%s] %3.0f%%", bar, progress*100)
}

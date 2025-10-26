package tui

import (
	"fmt"
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
	items    []FileItem
	cursor   int
	width    int
	height   int
	selected map[string]bool
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
	b.selected[path] = !b.selected[path]
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
	if len(b.items) == 0 {
		return Dimmed("No files found")
	}

	view := Title("Files") + "\n\n"

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

		checkbox := "☐ "
		if b.selected[item.Path] {
			checkbox = Success("☑ ")
		}

		name := item.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		view += cursor + checkbox + name + "\n"
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
		view += fmt.Sprintf("%s %s %s\n", Dimmed(timestamp), level, log.Message)
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
	return Title("Help") + "\n\nPress ? to close"
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

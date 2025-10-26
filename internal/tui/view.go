package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the TUI
func (m *Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.quitting {
		return "Shutting down gracefully...\n"
	}

	var content string

	// Render current view
	switch m.currentView {
	case ViewBrowser:
		content = m.renderBrowserView()
	case ViewDashboard:
		content = m.renderDashboardView()
	case ViewLogs:
		content = m.renderLogsView()
	case ViewHelp:
		content = m.renderHelpView()
	}

	// Build full view with header and footer
	header := m.renderHeader()
	footer := m.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		content,
		footer,
	)
}

// renderHeader renders the header bar
func (m *Model) renderHeader() string {
	if m.header != nil {
		return m.header.View()
	}

	// Fallback simple header
	title := HeaderStyle.Render("Javinizer TUI")

	workers := fmt.Sprintf("Workers: %d/%d",
		m.stats.Running,
		m.config.Performance.MaxWorkers)

	progress := fmt.Sprintf("Progress: %.0f%%", m.stats.OverallProgress*100)

	success := fmt.Sprintf("%s %d", Success("✓"), m.stats.Success)
	failed := ""
	if m.stats.Failed > 0 {
		failed = fmt.Sprintf("%s %d", Error("✗"), m.stats.Failed)
	}

	stats := StatusStyle.Render(
		strings.Join([]string{workers, progress, success, failed}, " │ "),
	)

	// Pad to full width
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(stats)
	if padding < 0 {
		padding = 0
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		title,
		strings.Repeat(" ", padding),
		stats,
	)
}

// renderFooter renders the footer with keybindings
func (m *Model) renderFooter() string {
	var keys []string

	switch m.currentView {
	case ViewBrowser:
		keys = []string{
			helpKey("↑↓/jk", "navigate"),
			helpKey("space", "select"),
			helpKey("a/A", "sel all/none"),
			helpKey("enter", "process"),
			helpKey("tab", "switch view"),
			helpKey("?", "help"),
			helpKey("q", "quit"),
		}
	case ViewDashboard:
		keys = []string{
			helpKey("tab", "switch view"),
			helpKey("?", "help"),
			helpKey("q", "quit"),
		}
	case ViewLogs:
		keys = []string{
			helpKey("↑↓/jk", "scroll"),
			helpKey("g/G", "top/bottom"),
			helpKey("a", "auto-scroll"),
			helpKey("tab", "switch view"),
			helpKey("?", "help"),
			helpKey("q", "quit"),
		}
	case ViewHelp:
		keys = []string{
			helpKey("?", "close help"),
			helpKey("q", "quit"),
		}
	}

	return HelpSeparatorStyle.Render(strings.Join(keys, " │ "))
}

// renderBrowserView renders the file browser view
func (m *Model) renderBrowserView() string {
	if m.browser != nil && m.taskList != nil {
		// Split screen: browser on left, tasks on right
		browserView := m.browser.View()
		taskView := m.taskList.View()

		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			BorderStyle.Width(m.width/2-2).Render(browserView),
			BorderStyle.Width(m.width/2-2).Render(taskView),
		)
	}

	// Fallback simple view
	return m.renderSimpleBrowser()
}

// renderDashboardView renders the dashboard view
func (m *Model) renderDashboardView() string {
	if m.dashboard != nil {
		return m.dashboard.View()
	}

	// Fallback simple dashboard
	return m.renderSimpleDashboard()
}

// renderLogsView renders the logs view
func (m *Model) renderLogsView() string {
	if m.logViewer != nil {
		return m.logViewer.View()
	}

	// Fallback simple logs
	return m.renderSimpleLogs()
}

// renderHelpView renders the help view
func (m *Model) renderHelpView() string {
	if m.helpView != nil {
		return m.helpView.View()
	}

	// Fallback simple help
	return m.renderSimpleHelp()
}

// Fallback renderers (simple text-based views)

func (m *Model) renderSimpleBrowser() string {
	var b strings.Builder

	b.WriteString(Title("File Browser") + "\n\n")

	if len(m.files) == 0 {
		b.WriteString(Dimmed("No files found\n"))
		return b.String()
	}

	// Show up to 20 files
	start := m.cursor - 10
	if start < 0 {
		start = 0
	}
	end := start + 20
	if end > len(m.files) {
		end = len(m.files)
	}

	for i := start; i < end; i++ {
		file := m.files[i]
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}

		checkbox := "☐"
		if file.Selected {
			checkbox = Success("☑")
		}

		name := file.Name
		if len(name) > 40 {
			name = name[:37] + "..."
		}

		line := fmt.Sprintf("%s %s %s", cursor, checkbox, name)
		if file.Matched {
			line += Dimmed(fmt.Sprintf(" (%s)", file.ID))
		}

		b.WriteString(line + "\n")
	}

	b.WriteString(fmt.Sprintf("\n%d files, %d selected\n",
		len(m.files), len(m.selectedFiles)))

	return b.String()
}

func (m *Model) renderSimpleDashboard() string {
	var b strings.Builder

	b.WriteString(Title("Dashboard") + "\n\n")

	b.WriteString(fmt.Sprintf("Total Tasks:    %d\n", m.stats.Total))
	b.WriteString(fmt.Sprintf("Running:        %s\n", RunningBadge.Render(fmt.Sprintf("%d", m.stats.Running))))
	b.WriteString(fmt.Sprintf("Success:        %s\n", Success(fmt.Sprintf("%d", m.stats.Success))))
	if m.stats.Failed > 0 {
		b.WriteString(fmt.Sprintf("Failed:         %s\n", Error(fmt.Sprintf("%d", m.stats.Failed))))
	}
	b.WriteString(fmt.Sprintf("\nProgress:       %.1f%%\n", m.stats.OverallProgress*100))
	b.WriteString(fmt.Sprintf("Elapsed:        %v\n", m.elapsedTime.Round(time.Second)))

	return b.String()
}

func (m *Model) renderSimpleLogs() string {
	var b strings.Builder

	b.WriteString(Title("Operation Logs") + "\n\n")

	if len(m.logs) == 0 {
		b.WriteString(Dimmed("No logs yet\n"))
		return b.String()
	}

	// Show last 20 logs
	start := len(m.logs) - 20
	if start < 0 {
		start = 0
	}

	for i := start; i < len(m.logs); i++ {
		log := m.logs[i]
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

		level := levelStyle.Render(fmt.Sprintf("%-5s", strings.ToUpper(log.Level)))
		b.WriteString(fmt.Sprintf("%s %s %s\n", Dimmed(timestamp), level, log.Message))
	}

	return b.String()
}

func (m *Model) renderSimpleHelp() string {
	var b strings.Builder

	b.WriteString(Title("Javinizer TUI - Help") + "\n\n")

	b.WriteString(Subtitle("Global Keybindings") + "\n")
	b.WriteString(helpLine("q, Ctrl+C", "Quit application"))
	b.WriteString(helpLine("?", "Toggle help"))
	b.WriteString(helpLine("tab", "Switch view"))
	b.WriteString(helpLine("1", "File browser"))
	b.WriteString(helpLine("2", "Dashboard"))
	b.WriteString(helpLine("3", "Logs"))

	b.WriteString("\n" + Subtitle("File Browser") + "\n")
	b.WriteString(helpLine("↑↓, j/k", "Navigate files"))
	b.WriteString(helpLine("space", "Toggle selection"))
	b.WriteString(helpLine("a", "Select all"))
	b.WriteString(helpLine("A", "Deselect all"))
	b.WriteString(helpLine("enter", "Start processing"))
	b.WriteString(helpLine("p", "Pause/resume"))

	b.WriteString("\n" + Subtitle("Logs View") + "\n")
	b.WriteString(helpLine("↑↓, j/k", "Scroll"))
	b.WriteString(helpLine("g", "Go to top"))
	b.WriteString(helpLine("G", "Go to bottom"))
	b.WriteString(helpLine("a", "Toggle auto-scroll"))

	return b.String()
}

// Helper functions

func helpKey(key, desc string) string {
	return HelpKeyStyle.Render(key) + ":" + HelpDescStyle.Render(desc)
}

func helpLine(key, desc string) string {
	return fmt.Sprintf("  %s  %s\n",
		HelpKeyStyle.Width(15).Render(key),
		HelpDescStyle.Render(desc))
}

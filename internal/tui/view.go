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
	case ViewSettings:
		content = m.renderSettingsView()
	case ViewHelp:
		content = m.renderHelpView()
	}

	// Build full view with header and footer
	header := m.renderHeader()
	footer := m.renderFooter()

	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		content,
		footer,
	)

	// Show manual search modal overlay if active (takes priority)
	if m.showingManualSearch {
		return RenderManualSearchModal(m)
	}

	// Show folder picker overlay if active
	if m.showingFolderPicker {
		return m.renderFolderPickerOverlay(mainView)
	}

	return mainView
}

// renderHeader renders the header bar
func (m *Model) renderHeader() string {
	// Title bar with dry-run indicator and processing status
	titleText := "Javinizer TUI"
	if m.dryRun {
		titleText += " " + Warning("[DRY RUN]")
	}
	if m.isProcessing {
		// Add spinning indicator - calculate elapsed time directly for smooth animation
		spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		elapsed := time.Since(m.startTime)
		spinner := spinners[int(elapsed.Milliseconds()/100)%len(spinners)]
		titleText += " " + RunningBadge.Render(spinner+" Processing")
	}
	title := HeaderStyle.Render(titleText)

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

	titleBar := lipgloss.JoinHorizontal(
		lipgloss.Top,
		title,
		strings.Repeat(" ", padding),
		stats,
	)

	// Tabs
	tabs := m.renderTabs()

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, tabs)
}

// renderTabs renders the tab bar
func (m *Model) renderTabs() string {
	var tabItems []string

	views := []struct {
		view ViewMode
		name string
		key  string
	}{
		{ViewBrowser, "Browser", "1"},
		{ViewDashboard, "Dashboard", "2"},
		{ViewLogs, "Logs", "3"},
		{ViewSettings, "Settings", "4"},
	}

	for _, v := range views {
		tabText := fmt.Sprintf("%s %s", v.key, v.name)
		if m.currentView == v.view {
			tabItems = append(tabItems, ActiveTabStyle.Render(tabText))
		} else {
			tabItems = append(tabItems, TabStyle.Render(tabText))
		}
	}

	return strings.Join(tabItems, "")
}

// renderFooter renders the footer with keybindings
func (m *Model) renderFooter() string {
	var keys []string

	switch m.currentView {
	case ViewBrowser:
		keys = []string{
			helpKey("f", "source"),
			helpKey("o", "output"),
			helpKey("r", "refresh"),
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
	case ViewSettings:
		keys = []string{
			helpKey("↑↓/jk", "navigate"),
			helpKey("space", "toggle"),
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
	if m.browser != nil && m.taskList != nil && m.console != nil {
		// Calculate available content height (total height - header - tabs - footer)
		contentHeight := m.height - 6 // Approximate space for header, tabs, footer

		// Split vertically: 60% for tasks, 40% for console
		taskHeight := contentHeight * 6 / 10
		consoleHeight := contentHeight * 4 / 10

		// Ensure minimum heights
		if taskHeight < 10 {
			taskHeight = 10
		}
		if consoleHeight < 8 {
			consoleHeight = 8
		}

		// Split screen: browser on left, tasks + console on right
		browserView := m.browser.View()
		taskView := m.taskList.View()
		consoleView := m.console.View()

		// Stack tasks and console vertically on the right with fixed heights
		rightPanel := lipgloss.JoinVertical(
			lipgloss.Left,
			BorderStyle.Width(m.width/2-2).Height(taskHeight).Render(taskView),
			BorderStyle.Width(m.width/2-2).Height(consoleHeight).Render(consoleView),
		)

		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			BorderStyle.Width(m.width/2-2).Render(browserView),
			rightPanel,
		)
	}

	// Fallback simple view
	return m.renderSimpleBrowser()
}

// renderDashboardView renders the dashboard view
func (m *Model) renderDashboardView() string {
	var content string

	if m.dashboard != nil {
		content = m.dashboard.View()
	} else {
		// Fallback simple dashboard
		content = m.renderSimpleDashboard()
	}

	// Add completion banner if processing just finished
	if m.processingComplete && !m.isProcessing {
		banner := m.renderCompletionBanner()
		content = lipgloss.JoinVertical(lipgloss.Left, banner, "", content)
	}

	return content
}

// renderLogsView renders the logs view
func (m *Model) renderLogsView() string {
	if m.logViewer != nil {
		return m.logViewer.View()
	}

	// Fallback simple logs
	return m.renderSimpleLogs()
}

// renderSettingsView renders the settings view
func (m *Model) renderSettingsView() string {
	if m.settingsView != nil {
		// Update settings state before rendering
		m.settingsView.UpdateSettings(
			m.settingsCursor,
			m.dryRun,
			m.forceUpdate,
			m.forceRefresh,
			m.moveFiles,
			m.scrapeEnabled,
			m.downloadEnabled,
			m.downloadExtrafanart,
			m.organizeEnabled,
			m.nfoEnabled,
			m.updateMode,
		)
		return m.settingsView.View()
	}

	// Fallback simple settings
	return m.renderSimpleSettings()
}

// renderHelpView renders the help view
func (m *Model) renderHelpView() string {
	if m.helpView != nil {
		return m.helpView.View()
	}

	// Fallback simple help
	return m.renderSimpleHelp()
}

// renderCompletionBanner renders a completion notification banner
func (m *Model) renderCompletionBanner() string {
	elapsed := m.completionTime.Sub(m.startTime).Round(time.Second)

	// Build summary message
	var summary strings.Builder
	summary.WriteString(Success("✓ Processing Complete! "))

	// Show file count
	summary.WriteString(fmt.Sprintf("Processed %d files in %v", m.totalFilesCount, elapsed))

	// Show success/failed counts
	if m.stats.Success > 0 || m.stats.Failed > 0 {
		summary.WriteString(" (")
		if m.stats.Success > 0 {
			summary.WriteString(Success(fmt.Sprintf("%d succeeded", m.stats.Success)))
		}
		if m.stats.Failed > 0 {
			if m.stats.Success > 0 {
				summary.WriteString(", ")
			}
			summary.WriteString(Error(fmt.Sprintf("%d failed", m.stats.Failed)))
		}
		summary.WriteString(")")
	}

	// Add navigation hints
	hints := Dimmed("  •  Press '1' or 'b' to return to browser  •  Press '3' for logs  •  Press 'd' to dismiss")

	// Style the banner
	bannerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")).
		Padding(0, 1).
		Width(m.width - 4)

	content := summary.String() + "\n" + hints
	return bannerStyle.Render(content)
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
	b.WriteString(helpLine("f", "Change scan folder"))
	b.WriteString(helpLine("r", "Refresh/rescan current folder"))
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

func (m *Model) renderSimpleSettings() string {
	var b strings.Builder

	b.WriteString(Title("Settings") + "\n\n")

	settings := []struct {
		name    string
		desc    string
		enabled bool
	}{
		{"Dry Run", "Preview mode - don't make actual changes", m.dryRun},
		{"Force Update", "Replace existing files (images, NFO)", m.forceUpdate},
		{"Force Refresh", "Clear DB cache and rescrape metadata", m.forceRefresh},
		{"Move Files", "Move instead of copy (default: copy)", m.moveFiles},
		{"Scrape Metadata", "Fetch metadata from JAV sources", m.scrapeEnabled},
		{"Download Media", "Download covers, screenshots, trailers", m.downloadEnabled},
		{"Download Extrafanart", "Download extrafanart/screenshots to subfolder", m.downloadExtrafanart},
		{"Organize Files", "Move/copy files to organized structure", m.organizeEnabled},
		{"Generate NFO", "Create NFO files for media centers", m.nfoEnabled},
	}

	for i, setting := range settings {
		cursorStr := "  "
		if i == m.settingsCursor {
			cursorStr = "> "
		}

		checkbox := "☐"
		if setting.enabled {
			checkbox = Success("☑")
		}

		b.WriteString(fmt.Sprintf("%s%s %s\n", cursorStr, checkbox, setting.name))
		b.WriteString(fmt.Sprintf("   %s\n\n", Dimmed(setting.desc)))
	}

	b.WriteString("\n" + Dimmed("Changes take effect on next processing run"))
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

// renderFolderPickerOverlay renders the folder picker as a modal overlay
func (m *Model) renderFolderPickerOverlay(background string) string {
	// Calculate modal dimensions
	modalWidth := m.width - 20
	if modalWidth > 80 {
		modalWidth = 80
	}
	modalHeight := m.height - 10
	if modalHeight > 25 {
		modalHeight = 25
	}

	var b strings.Builder

	// Title based on mode
	title := "Select Source Folder"
	if m.folderPickerMode == "dest" {
		title = "Select Output Folder"
	}
	b.WriteString(Title(title) + "\n\n")

	// Current path
	displayPath := m.folderPickerPath
	if len(displayPath) > modalWidth-10 {
		displayPath = "..." + displayPath[len(displayPath)-(modalWidth-13):]
	}
	b.WriteString(Dimmed("Current: ") + Highlight(displayPath) + "\n\n")

	// Folder list
	if len(m.folderPickerItems) == 0 {
		b.WriteString(Dimmed("No folders found\n"))
	} else {
		// Calculate visible range
		visibleHeight := modalHeight - 8
		start := m.folderPickerCursor - visibleHeight/2
		if start < 0 {
			start = 0
		}
		end := start + visibleHeight
		if end > len(m.folderPickerItems) {
			end = len(m.folderPickerItems)
			start = end - visibleHeight
			if start < 0 {
				start = 0
			}
		}

		for i := start; i < end; i++ {
			item := m.folderPickerItems[i]
			cursor := "  "
			if i == m.folderPickerCursor {
				cursor = "> "
			}

			icon := "📁 "
			if item.Name == ".." {
				icon = "⬆️  "
			}

			name := item.Name
			if len(name) > modalWidth-10 {
				name = name[:modalWidth-13] + "..."
			}

			line := cursor + icon + name
			if i == m.folderPickerCursor {
				line = SelectedItemStyle.Render(line)
			} else {
				line = UnselectedItemStyle.Render(line)
			}

			b.WriteString(line + "\n")
		}
	}

	// Instructions
	b.WriteString("\n" + Dimmed("↑↓/jk: navigate  Enter: select folder  Space: choose current  Esc: cancel"))

	// Create modal panel
	modal := PanelStyle.
		Width(modalWidth).
		Height(modalHeight).
		Render(b.String())

	// Center the modal
	modalWithPadding := lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)

	// Layer modal over background (simple approach - just return modal for now)
	return modalWithPadding
}

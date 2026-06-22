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
	switch m.viewMgr.currentView() {
	case viewBrowser:
		content = m.renderBrowserView()
	case viewDashboard:
		content = m.renderDashboardView()
	case viewLogs:
		content = m.renderLogsView()
	case viewSettings:
		content = m.renderSettingsView()
	case viewHelp:
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
	if m.manualSearch.showing {
		return m.manualSearch.View()
	}

	// Show actress merge modal overlay if active
	if m.actressMergeCtl.Showing() {
		return m.actressMergeCtl.View()
	}

	// Show folder picker overlay if active
	if m.folderPickCtl.Showing() {
		return m.folderPickCtl.View()
	}

	return mainView
}

// renderHeader renders the header bar
func (m *Model) renderHeader() string {
	// title bar with dry-run indicator and processing status
	titleText := "Javinizer TUI"
	if m.settingsMgr.get().DryRun {
		titleText += " " + warning("[DRY RUN]")
	}
	if m.taskTracker.isProcessing.Load() {
		// Add spinning indicator - calculate elapsed time directly for smooth animation
		spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		elapsed := time.Since(m.startTime)
		spinner := spinners[int(elapsed.Milliseconds()/100)%len(spinners)]
		titleText += " " + runningBadge.Render(spinner+" Processing")
	}
	title := headerStyle.Render(titleText)

	workers := fmt.Sprintf("Workers: %d/%d",
		m.eventSub.Stats().Running,
		m.modelCfg.MaxWorkers)

	progress := fmt.Sprintf("Progress: %.0f%%", m.eventSub.Stats().OverallProgress*100)

	success := fmt.Sprintf("%s %d", success("✓"), m.eventSub.Stats().success)
	failed := ""
	if m.eventSub.Stats().Failed > 0 {
		failed = fmt.Sprintf("%s %d", errorStyled("✗"), m.eventSub.Stats().Failed)
	}

	stats := statusStyle.Render(
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
		view viewMode
		name string
		key  string
	}{
		{viewBrowser, "browser", "1"},
		{viewDashboard, "dashboard", "2"},
		{viewLogs, "Logs", "3"},
		{viewSettings, "Settings", "4"},
	}

	for _, v := range views {
		tabText := fmt.Sprintf("%s %s", v.key, v.name)
		if m.viewMgr.currentView() == v.view {
			tabItems = append(tabItems, activeTabStyle.Render(tabText))
		} else {
			tabItems = append(tabItems, tabStyle.Render(tabText))
		}
	}

	return strings.Join(tabItems, "")
}

// renderFooter renders the footer with keybindings
func (m *Model) renderFooter() string {
	var keys []string

	switch m.viewMgr.currentView() {
	case viewBrowser:
		keys = []string{
			helpKey("f", "source"),
			helpKey("o", "output"),
			helpKey("m", "manual search"),
			helpKey("M", "merge actress"),
			helpKey("r", "refresh"),
			helpKey("↑↓/jk", "navigate"),
			helpKey("space", "select"),
			helpKey("a/A", "sel all/none"),
			helpKey("enter", "process"),
			helpKey("tab", "switch view"),
			helpKey("?", "help"),
			helpKey("q", "quit"),
		}
	case viewDashboard:
		keys = []string{
			helpKey("tab", "switch view"),
			helpKey("?", "help"),
			helpKey("q", "quit"),
		}
	case viewLogs:
		keys = []string{
			helpKey("↑↓/jk", "scroll"),
			helpKey("g/G", "top/bottom"),
			helpKey("a", "auto-scroll"),
			helpKey("tab", "switch view"),
			helpKey("?", "help"),
			helpKey("q", "quit"),
		}
	case viewSettings:
		keys = []string{
			helpKey("↑↓/jk", "navigate"),
			helpKey("space", "toggle"),
			helpKey("tab", "switch view"),
			helpKey("?", "help"),
			helpKey("q", "quit"),
		}
	case viewHelp:
		keys = []string{
			helpKey("?", "close help"),
			helpKey("q", "quit"),
		}
	}

	return helpSeparatorStyle.Render(strings.Join(keys, " │ "))
}

// renderBrowserView renders the file browser view
func (m *Model) renderBrowserView() string {
	if m.browser == nil {
		panic("tui: browser component is nil — this is a programmer error; check Model construction")
	}
	if m.taskList == nil {
		panic("tui: taskList component is nil — this is a programmer error; check Model construction")
	}
	if m.console == nil {
		panic("tui: console component is nil — this is a programmer error; check Model construction")
	}

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
		borderStyle.Width(m.width/2-2).Height(taskHeight).Render(taskView),
		borderStyle.Width(m.width/2-2).Height(consoleHeight).Render(consoleView),
	)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		borderStyle.Width(m.width/2-2).Render(browserView),
		rightPanel,
	)
}

// renderDashboardView renders the dashboard view
func (m *Model) renderDashboardView() string {
	if m.dashboard == nil {
		panic("tui: dashboard component is nil — this is a programmer error; check Model construction")
	}

	content := m.dashboard.View()

	// Add completion banner if processing just finished
	if m.taskTracker.processingComplete.Load() && !m.taskTracker.isProcessing.Load() {
		banner := m.renderCompletionBanner()
		content = lipgloss.JoinVertical(lipgloss.Left, banner, "", content)
	}

	return content
}

// renderLogsView renders the logs view
func (m *Model) renderLogsView() string {
	if m.logViewer == nil {
		panic("tui: logViewer component is nil — this is a programmer error; check Model construction")
	}
	// Push current log state to the renderer before rendering
	m.logViewer.SetLogs(m.logState.logs, m.logState.logScroll, m.logState.autoScroll)
	return m.logViewer.View()
}

// renderSettingsView renders the settings view
func (m *Model) renderSettingsView() string {
	if m.settingsView == nil {
		panic("tui: settingsView component is nil — this is a programmer error; check Model construction")
	}

	// Update settings state before rendering
	m.settingsView.UpdateSettings(m.settingsMgr.cursorPos(), m.settingsMgr.get())
	return m.settingsView.View()
}

// renderHelpView renders the help view
func (m *Model) renderHelpView() string {
	if m.helpView == nil {
		panic("tui: helpView component is nil — this is a programmer error; check Model construction")
	}
	return m.helpView.View()
}

// renderCompletionBanner renders a completion notification banner
func (m *Model) renderCompletionBanner() string {
	elapsed := m.taskTracker.completionTime.Sub(m.startTime).Round(time.Second)

	// Build summary message
	var summary strings.Builder
	summary.WriteString(success("✓ Processing Complete! "))

	// Show file count
	fmt.Fprintf(&summary, "Processed %d files in %v", m.taskTracker.totalFilesCount, elapsed)

	// Show success/failed counts
	if m.eventSub.Stats().success > 0 || m.eventSub.Stats().Failed > 0 {
		summary.WriteString(" (")
		if m.eventSub.Stats().success > 0 {
			summary.WriteString(success(fmt.Sprintf("%d succeeded", m.eventSub.Stats().success)))
		}
		if m.eventSub.Stats().Failed > 0 {
			if m.eventSub.Stats().success > 0 {
				summary.WriteString(", ")
			}
			summary.WriteString(errorStyled(fmt.Sprintf("%d failed", m.eventSub.Stats().Failed)))
		}
		summary.WriteString(")")
	}

	// Add navigation hints
	hints := dimmed("  •  Press '1' or 'b' to return to browser  •  Press '3' for logs  •  Press 'd' to dismiss")

	// Style the banner
	bannerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")).
		Padding(0, 1).
		Width(m.width - 4)

	content := summary.String() + "\n" + hints
	return bannerStyle.Render(content)
}

// Helper functions

func helpKey(key, desc string) string {
	return helpKeyStyle.Render(key) + ":" + helpDescStyle.Render(desc)
}
